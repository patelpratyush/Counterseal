#!/usr/bin/env python3
"""Run Go tests against an isolated, disposable local PostgreSQL cluster."""
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile


def main():
    for binary in ('initdb', 'pg_ctl'):
        if not shutil.which(binary):
            sys.exit(f'{binary} is required on PATH')
    with tempfile.TemporaryDirectory(prefix='hg-pg-', dir='/tmp') as directory:
        root = pathlib.Path(directory)
        data = root / 'data'
        subprocess.run(['initdb', '-D', str(data), '-U', 'postgres', '--auth=trust',
                        '--no-locale', '--encoding=UTF8'], check=True, stdout=subprocess.DEVNULL)
        # Unix socket only: no existing service or TCP port is touched.
        subprocess.run(['pg_ctl', '-D', str(data), '-l', str(root / 'postgres.log'),
                        '-o', f"-h '' -k {directory}", '-w', 'start'], check=True)
        try:
            env = dict(os.environ)
            env['HANDOFFGUARD_TEST_DATABASE_URL'] = f'postgresql://postgres@/postgres?host={directory}'
            command = sys.argv[1:] or ['go', 'test', '-race', './...']
            result = subprocess.run(command, env=env)
            return result.returncode
        finally:
            subprocess.run(['pg_ctl', '-D', str(data), '-m', 'immediate', '-w', 'stop'], check=True)


if __name__ == '__main__':
    sys.exit(main())
