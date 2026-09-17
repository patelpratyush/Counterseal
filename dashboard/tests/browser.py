"""Browser checks against real Go API data; invoked by scripts/smoke-dashboard.py."""
import json
import os
import pathlib
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from playwright.sync_api import sync_playwright, expect, TimeoutError as PlaywrightTimeout

ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE = os.environ['HANDOFFGUARD_SERVER_URL']
TOKEN = os.environ['HANDOFFGUARD_API_TOKEN']


def api(path, body):
    request = urllib.request.Request(BASE + path, data=json.dumps(body).encode(),
        headers={'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json'})
    try:
        response = urllib.request.urlopen(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, json.load(response)


def seed():
    env = {'version':'1', 'issuer':{'agent':'demo-operator'}, 'recipient':{'agent':'support-agent'},
        'purpose':'customer_refund', 'policy_version':'refund-v1', 'allowed_actions':['orders.read','refunds.create','email.send'],
        'denied_actions':['payments.export'], 'resources':{'orders':['48319']}, 'data_classes':['payment_metadata'],
        'approvals':[{'condition':'has(refund.amount) && refund.amount > 500', 'required_role':'refund_manager'}],
        'delegation':{'max_depth':2,'current_depth':0,'may_expand_authority':False},
        'expires_at':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()}
    status, root = api('/v1/envelopes', {'envelope':env})
    assert status == 201, root
    parent = root['envelope']
    for stage, actions in [('billing-agent',['refunds.create','email.send']),('notification-agent',['email.send'])]:
        child = json.loads(json.dumps(parent))
        for key in ['id','signature']:
            child.pop(key,None)
        child.update(issuer=parent['recipient'],recipient={'agent':stage},parent_envelope=parent['id'],allowed_actions=actions)
        child['delegation']['current_depth']+=1
        status, result = api('/v1/envelopes/'+parent['id']+'/delegate', {'child':child})
        assert status==201, result
        if stage=='billing-agent':
            bad=json.loads(json.dumps(child))
            bad['allowed_actions'].append('payments.export')
            denied,_=api('/v1/envelopes/'+parent['id']+'/delegate', {'child':bad})
            assert denied==403
        parent=result['envelope']
    return root['run_id']


def settle(page):
    # Next.js can hold speculative RSC prefetch streams open after the view is ready.
    try:
        page.wait_for_load_state('networkidle', timeout=3000)
    except PlaywrightTimeout:
        page.wait_for_load_state('domcontentloaded')
    page.evaluate('document.fonts.ready')


def main():
    run = seed()
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0))
        port=sock.getsockname()[1]
    url=f'http://localhost:{port}'
    env=dict(os.environ)
    env['HANDOFFGUARD_DASHBOARD_PASSWORD']=secrets.token_hex(16)
    env['HANDOFFGUARD_DASHBOARD_SECRET']=secrets.token_hex(32)
    env['NEXT_TELEMETRY_DISABLED']='1'
    with tempfile.TemporaryFile(mode='w+') as log:
        process=subprocess.Popen(['npm','run','start','--','--hostname','127.0.0.1','--port',str(port)],
            cwd=ROOT/'dashboard',env=env,stdout=log,stderr=log,start_new_session=True)
        try:
            for _ in range(200):
                try:
                    urllib.request.urlopen(url+'/login',timeout=1).close()
                    break
                except (OSError,urllib.error.URLError):
                    if process.poll() is not None:
                        log.seek(0);raise RuntimeError(log.read())
                    time.sleep(.1)
            else:
                log.seek(0);raise RuntimeError(log.read())
            with sync_playwright() as p:
                browser=p.chromium.launch(headless=True)
                page=browser.new_page(viewport={'width':1440,'height':1000})
                errors=[]
                page.on('pageerror',lambda error:errors.append(str(error)))
                page.goto(url+'/runs/'+run)
                expect(page).to_have_url(url+'/login')
                page.get_by_label('Viewer password').fill('wrong-password')
                page.get_by_role('button',name='Open console').click()
                expect(page.locator('.login-form [role=alert]')).to_contain_text('Incorrect viewer password')
                page.get_by_label('Viewer password').fill(env['HANDOFFGUARD_DASHBOARD_PASSWORD'])
                page.get_by_role('button',name='Open console').click()
                expect(page.get_by_role('heading',name='Authority overview.')).to_be_visible()
                settle(page)
                assert TOKEN not in page.content()
                page.screenshot(path='/tmp/handoffguard-overview.png',full_page=True)
                page.get_by_label('Search runs').fill('not-a-real-run')
                page.get_by_role('button',name='Search',exact=True).click()
                expect(page.get_by_role('heading',name='No matching runs')).to_be_visible()
                page.goto(url+'/runs/'+run)
                settle(page)
                expect(page.get_by_text('Authority map',exact=True)).to_be_visible()
                expect(page.locator('.react-flow__node')).to_have_count(4)
                page.get_by_role('button',name='Handoff 2 · DENY',exact=True).click()
                expect(page.get_by_role('heading',name='Delegation blocked')).to_be_visible()
                expect(page.locator('.violations')).to_contain_text('ACTION_EXPANDED')
                page.get_by_role('button',name='Handoff 1 · ALLOW',exact=True).click()
                expect(page.get_by_role('heading',name='Authority inherited')).to_be_visible()
                page.get_by_role('button',name='Verify audit',exact=True).click()
                expect(page.get_by_text('Audit valid',exact=True)).to_be_visible()
                page.screenshot(path='/tmp/handoffguard-run.png',full_page=True)
                page.get_by_role('tab',name='Decision history').click()
                expect(page.get_by_text('Delegation denied',exact=True)).to_be_visible()
                page.get_by_role('button',name='Toggle color theme').click()
                expect(page.locator('html')).to_have_class(__import__('re').compile(r'dark'))
                page.get_by_role('tab',name='Delegation graph').click()
                page.set_viewport_size({'width':390,'height':844})
                page.screenshot(path='/tmp/handoffguard-mobile.png',full_page=True)
                assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'), 'mobile overflow'
                page.set_viewport_size({'width':1440,'height':1000})
                page.get_by_role('button',name='Sign out').click()
                expect(page).to_have_url(url+'/login')
                page.goto(url+'/runs/'+run)
                expect(page).to_have_url(url+'/login')
                assert not errors, errors
                browser.close()
            print('PASS: login, search, graph, denied handoff, audit, theme, mobile, logout; no browser errors')
        finally:
            os.killpg(process.pid,signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid,signal.SIGKILL)
                process.wait()


if __name__=='__main__':
    main()
