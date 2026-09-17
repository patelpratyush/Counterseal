#!/usr/bin/env python3
"""Build the CLI and exercise the gateway against an isolated test database.

Run via: python3 scripts/test-postgres.py python3 scripts/smoke-gateway.py
"""
import os
import pathlib
import secrets
import signal
import subprocess
import tempfile
import time


def main(command=None):
    with tempfile.TemporaryDirectory(prefix='hg-gateway-smoke-') as directory:
        root = pathlib.Path(directory)
        binary = root / 'handoffguard'
        subprocess.run(['go', 'build', '-o', str(binary), './cmd/cli'], check=True)
        subprocess.run([str(binary), 'keygen', '--out', str(root / 'key')],
                       check=True, stdout=subprocess.DEVNULL)
        env = dict(os.environ)
        env['HANDOFFGUARD_DATABASE_URL'] = env['HANDOFFGUARD_TEST_DATABASE_URL']
        env['HANDOFFGUARD_API_TOKEN'] = secrets.token_hex(32)
        env['HANDOFFGUARD_BINARY'] = str(binary)
        with (root / 'server.log').open('w+') as log:
            process = subprocess.Popen([str(binary), 'server', '--key', str(root / 'key.priv'),
                                        '--addr', '127.0.0.1:0'], env=env, stdout=log, stderr=log)
            try:
                for _ in range(150):
                    log.seek(0)
                    line = log.readline()
                    if line.startswith('HandoffGuard listening on '):
                        break
                    if process.poll() is not None:
                        raise RuntimeError((root / 'server.log').read_text())
                    time.sleep(0.1)
                else:
                    raise RuntimeError('server startup timed out')
                env['HANDOFFGUARD_SERVER_URL'] = 'http://' + line.strip().split()[-1]
                subprocess.run(command or ['python3', 'scripts/demo-gateway.py'], env=env, check=True, timeout=180)
            finally:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                    raise
            if process.returncode != 0:
                raise RuntimeError((root / 'server.log').read_text())
        print('PASS: integration command and graceful server shutdown')


if __name__ == '__main__':
    main()
