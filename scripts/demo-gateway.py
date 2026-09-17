#!/usr/bin/env python3
"""Run the unsafe-refund vs guarded-refund demo against a running control API.

Requires only Python's standard library and a built HandoffGuard binary.
"""
import json
import os
import pathlib
import selectors
import subprocess
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone

ROOT = pathlib.Path(__file__).resolve().parents[1]
BINARY = str(pathlib.Path(os.environ.get('HANDOFFGUARD_BINARY', ROOT / 'handoffguard')).resolve())
BASE = os.environ.get('HANDOFFGUARD_SERVER_URL', 'http://127.0.0.1:8080').rstrip('/')
TOKEN = os.environ['HANDOFFGUARD_API_TOKEN']


def api(path, body=None, expected=200):
    request = urllib.request.Request(BASE + path, data=json.dumps(body).encode(),
        headers={'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json'})
    try:
        response = urllib.request.urlopen(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        result = json.load(response)
        if response.status != expected:
            raise RuntimeError(f'{path}: HTTP {response.status}: {result}')
        return result


class MCP:
    def __init__(self, args, env=None):
        self.process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        bufsize=0, env=env)
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)
        self.buffer = b''
        self.next_id = 0

    def __enter__(self):
        try:
            self.request('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {},
                'clientInfo': {'name': 'handoffguard-demo', 'version': '1'}})
            self.send({'jsonrpc': '2.0', 'method': 'notifications/initialized'})
            return self
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *_):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.selector.close()
        self.process.stdout.close()

    def send(self, value):
        self.process.stdin.write(json.dumps(value).encode() + b'\n')

    def request(self, method, params):
        self.next_id += 1
        id = self.next_id
        self.send({'jsonrpc': '2.0', 'id': id, 'method': method, 'params': params})
        while True:
            while b'\n' not in self.buffer:
                if not self.selector.select(timeout=35):
                    raise RuntimeError('MCP response timed out')
                chunk = os.read(self.process.stdout.fileno(), 65536)
                if not chunk:
                    raise RuntimeError('MCP subprocess closed its output')
                self.buffer += chunk
            line, self.buffer = self.buffer.split(b'\n', 1)
            message = json.loads(line)
            if message.get('id') == id and ('result' in message or 'error' in message):
                return message
            if 'method' in message and 'id' in message:
                self.send({'jsonrpc': '2.0', 'id': message['id'],
                           'error': {'code': -32601, 'message': 'Unsupported client request'}})

    def tool(self, name, arguments):
        response = self.request('tools/call', {'name': name, 'arguments': arguments})
        if 'error' in response:
            raise RuntimeError(response['error'])
        return response['result']


def main():
    args = {'order_id': '48319', 'amount': 825}
    safe_env = {k: v for k, v in os.environ.items() if not k.startswith('HANDOFFGUARD_')}
    with MCP([BINARY, 'demo-mcp'], env=safe_env) as direct:
        assert not direct.tool('refund.create', args).get('isError', False)
    print('Unsafe upstream: $825 simulated refund succeeds without approval', flush=True)

    now = datetime.now(timezone.utc)
    root = api('/v1/envelopes', {'envelope': {
        'version': '1', 'issuer': {'agent': 'support'}, 'recipient': {'agent': 'billing'},
        'purpose': 'customer_refund', 'policy_version': 'refund-v1',
        'allowed_actions': ['refunds.create', 'orders.read'], 'denied_actions': ['payments.export'],
        'resources': {'orders': ['48319']}, 'data_classes': ['payment_metadata'],
        'approvals': [{'condition': 'has(refund.amount) && refund.amount > 500',
                       'required_role': 'refund_manager'}],
        'delegation': {'max_depth': 2, 'current_depth': 0, 'may_expand_authority': False},
        'expires_at': (now + timedelta(hours=1)).isoformat(),
    }}, expected=201)
    id = root['envelope']['id']
    with MCP([BINARY, 'gateway', '--config', str(ROOT / 'examples/gateway-tools.json'),
              '--agent', 'billing', '--envelope', id, '--api-url', BASE,
              '--', BINARY, 'demo-mcp']) as guarded:
        listing = guarded.request('tools/list', {})['result']['tools']
        assert {tool['name'] for tool in listing} == {'refund.create', 'orders.get'}
        assert 'error' in guarded.request('tools/call', {'name': 'payment.export', 'arguments': {}})
        assert guarded.tool('refund.create', args)['isError']
        print('Gateway DENY: approval missing; export tool hidden', flush=True)
        api('/v1/approvals', {'envelope_id': id, 'action': 'refunds.create',
            'resource': 'orders:48319', 'arguments': args, 'approved_by': 'manager_193',
            'role': 'refund_manager', 'expires_at': (now + timedelta(minutes=10)).isoformat()}, expected=201)
        result = guarded.tool('refund.create', args)
        assert not result.get('isError', False)
        assert result['structuredContent']['refund_id'] == 'simulated_1'
        print('Gateway ALLOW: first upstream refund executes after approval', flush=True)
        assert guarded.tool('refund.create', args)['isError']
        print('Gateway DENY: approval replay', flush=True)
        assert not guarded.tool('orders.get', {'order_id': '48319'}).get('isError', False)
        api('/v1/envelopes/' + id + '/revoke')
        assert guarded.tool('orders.get', {'order_id': '48319'})['isError']
        print('Gateway DENY: revoked envelope', flush=True)
    audit = api('/v1/audit/' + root['run_id'] + '/verify')
    assert audit['status'] == 'VALID', audit
    print('Audit VALID:', root['run_id'], flush=True)


if __name__ == '__main__':
    main()
