#!/usr/bin/env python3
"""Start an isolated, persistent local HandoffGuard preview. Ctrl+C stops it."""
import argparse
import fcntl
import hashlib
import json
import os
import pathlib
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import webbrowser

ROOT = pathlib.Path(__file__).resolve().parents[1]
STATE = ROOT / '.local-preview'


def say(message):
    print(message, flush=True)


def run(command, log, env=None, cwd=ROOT):
    subprocess.run(command, cwd=cwd, env=env, stdout=log, stderr=log, check=True)


def stop(process):
    if process is None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait()


def free_port(preferred):
    with socket.socket() as sock:
        try:
            sock.bind(('127.0.0.1', preferred))
        except OSError:
            sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def wait_http(url, process):
    for _ in range(600):
        if process.poll() is not None:
            raise RuntimeError('A service exited during startup. Check .local-preview logs.')
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(.2)
    raise RuntimeError('Service startup timed out. Check .local-preview logs.')


def api(base, token, path):
    req = urllib.request.Request(base + path, headers={'Authorization': 'Bearer ' + token})
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--no-open', action='store_true', help='Do not open a browser')
    parser.add_argument('--no-seed', action='store_true', help='Do not create the first simulated agent run')
    parser.add_argument('--check', action='store_true', help='Check startup, then stop all services')
    parser.add_argument('--port', type=int, default=3000, help='Preferred dashboard port; uses a free port if busy')
    args = parser.parse_args()
    if not 0 <= args.port <= 65535:
        parser.error('--port must be between 0 and 65535')

    # Homebrew does not always add PostgreSQL executables to a fresh shell's PATH.
    for directory in ('/usr/local/opt/postgresql@18/bin', '/opt/homebrew/opt/postgresql@18/bin'):
        if pathlib.Path(directory).is_dir():
            os.environ['PATH'] = directory + os.pathsep + os.environ.get('PATH', '')
    required = ['go', 'node', 'npm', 'initdb', 'pg_ctl'] + ([] if args.no_seed else ['uv'])
    missing = [tool for tool in required if not shutil.which(tool)]
    if missing:
        raise RuntimeError('Missing tools: ' + ', '.join(missing) + '. Install them first (macOS: brew install go node postgresql@18 uv).')

    os.umask(0o077)
    STATE.mkdir(mode=0o700, exist_ok=True)
    with (STATE / 'launcher.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError('The local preview is already running. Use its terminal to stop it.') from None
        return launch(args)


def launch(args):
    config_path = STATE / 'credentials.json'
    if not config_path.exists():
        with config_path.open('x') as file:
            json.dump({'api_token': secrets.token_hex(32), 'password': secrets.token_hex(16),
                       'secret': secrets.token_hex(32)}, file)
    config = json.loads(config_path.read_text())
    if any(not isinstance(config.get(k), str) or len(config[k]) < n
           for k, n in [('api_token',32),('password',16),('secret',32)]):
        raise RuntimeError('Invalid .local-preview/credentials.json; restore valid local credentials.')
    data, binary = STATE / 'postgres', STATE / 'handoffguard'
    if data.exists() and subprocess.run(['pg_ctl','-D',str(data),'status'],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise RuntimeError(f'A preview database is already running. Stop it first: pg_ctl -D "{data}" -m fast stop')

    backend = dashboard = None
    database_started = False
    env = dict(os.environ)
    env.update(HANDOFFGUARD_API_TOKEN=config['api_token'],
        HANDOFFGUARD_DASHBOARD_PASSWORD=config['password'], HANDOFFGUARD_DASHBOARD_SECRET=config['secret'],
        HANDOFFGUARD_BINARY=str(binary), NEXT_TELEMETRY_DISABLED='1', NODE_ENV='development',
        HANDOFFGUARD_LOCAL_PREVIEW='1')
    with tempfile.TemporaryDirectory(prefix='hg-preview-', dir='/tmp') as socket_dir, \
         (STATE / 'setup.log').open('w') as setup_log, \
         (STATE / 'api.log').open('w+') as api_log, \
         (STATE / 'dashboard.log').open('w') as dashboard_log:
        try:
            say('Preparing local preview…')
            if not (data / 'PG_VERSION').exists():
                run(['initdb','-D',str(data),'-U','postgres','--auth=trust','--no-locale','--encoding=UTF8'],setup_log)
            # Unix socket in a private directory; no global service or database is touched.
            run(['pg_ctl','-D',str(data),'-l',str(STATE / 'postgres.log'),'-o',f"-h '' -k {socket_dir}",'-w','start'],setup_log)
            database_started = True
            env['HANDOFFGUARD_DATABASE_URL'] = 'postgresql://postgres@/postgres?' + urllib.parse.urlencode({'host':socket_dir})
            run(['go','build','-o',str(binary),'./cmd/cli'],setup_log)
            key = STATE / 'server'
            if not key.with_suffix('.priv').exists():
                run([str(binary),'keygen','--out',str(key)],setup_log)
            backend = subprocess.Popen([str(binary),'server','--key',str(key.with_suffix('.priv')),'--addr','127.0.0.1:0'],
                cwd=ROOT,env=env,stdout=api_log,stderr=api_log,start_new_session=True)
            for _ in range(200):
                api_log.seek(0)
                line = api_log.readline()
                if line.startswith('HandoffGuard listening on '):
                    base = 'http://' + line.strip().split()[-1]
                    break
                if backend.poll() is not None:
                    raise RuntimeError('API startup failed; see .local-preview/api.log.')
                time.sleep(.1)
            else:
                raise RuntimeError('API startup timed out; see .local-preview/api.log.')
            env['HANDOFFGUARD_SERVER_URL'] = base
            overview = api(base,config['api_token'],'/v1/dashboard/overview')
            if not args.no_seed and overview['stats']['runs'] == 0:
                say('Adding a simulated Support → Billing → Notification run…')
                run(['uv','run','--directory','integrations/openai-agents','--frozen','python','demo.py'],setup_log,env)

            lock_hash = hashlib.sha256((ROOT / 'dashboard/package-lock.json').read_bytes()).hexdigest()
            stamp = STATE / 'npm-lock-hash'
            if not (ROOT / 'dashboard/node_modules/next').exists() or not stamp.exists() or stamp.read_text() != lock_hash:
                say('Installing dashboard dependencies (first start or changed lockfile)…')
                run(['npm','ci'],setup_log,env,ROOT / 'dashboard')
                stamp.write_text(lock_hash)
            port = free_port(args.port)
            url = f'http://localhost:{port}'
            say('Starting dashboard…')
            dashboard = subprocess.Popen(['npm','run','dev','--','--hostname','127.0.0.1','--port',str(port)],
                cwd=ROOT / 'dashboard',env=env,stdout=dashboard_log,stderr=dashboard_log,start_new_session=True)
            wait_http(url + '/login',dashboard)
            if args.check:
                say('Startup check passed. Stopping preview services.')
                return 0
            say(f'\nDashboard: {url}\nPassword:  {config["password"]}\n\nKeep this terminal open. Press Ctrl+C to stop.\nYour preview data is saved in .local-preview/.')
            if not args.no_open:
                webbrowser.open(url)
            while True:
                if backend.poll() is not None or dashboard.poll() is not None:
                    raise RuntimeError('A preview service stopped. See .local-preview logs.')
                time.sleep(1)
        finally:
            stop(dashboard)
            stop(backend)
            if database_started:
                result = subprocess.run(['pg_ctl','-D',str(data),'-m','fast','-w','stop'],stdout=setup_log,stderr=setup_log)
                if result.returncode:
                    say('Database shutdown failed; see .local-preview/setup.log.')
            say('Preview stopped. Saved data and credentials are preserved.')


if __name__ == '__main__':
    def interrupted(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(0)
    except (RuntimeError, subprocess.CalledProcessError, OSError, ValueError) as error:
        sys.exit(f'{error}\nSetup details: {STATE / "setup.log"}')
