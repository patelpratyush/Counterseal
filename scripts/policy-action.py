#!/usr/bin/env python3
"""GitHub Action adapter. Inputs are data, never interpolated into shell code."""
import html
import json
import os
import pathlib
import subprocess
import sys
import tempfile


def check():
    try:
        workspace = pathlib.Path(os.environ.get('GITHUB_WORKSPACE', '.')).resolve()
        parent, child = os.environ.get('HG_PARENT', ''), os.environ.get('HG_CHILD', '')
        if not parent or not child:
            raise ValueError('Both parent and child paths are required')
        command = [os.environ['HG_BINARY'], 'policy', 'diff',
                   str((workspace / parent).resolve()), str((workspace / child).resolve()), '--format', 'json']
        for variable, flag in [('HG_PARENT_KEY','--parent-key'), ('HG_CHILD_KEY','--child-key')]:
            if os.environ.get(variable):
                command.extend([flag, str((workspace / os.environ[variable]).resolve())])
        result = subprocess.run(command, capture_output=True, text=True, timeout=60)
        try:
            report = json.loads(result.stdout)
        except json.JSONDecodeError as error:
            raise ValueError(result.stderr.strip() or 'Invalid JSON from policy checker') from error
        if not isinstance(report, dict) or report.get('decision') not in ('ALLOW','DENY') or not isinstance(report.get('violations'), list):
            raise ValueError('Invalid response from policy checker')
        if report['decision']=='ALLOW' and (result.returncode != 0 or report['violations']):
            raise ValueError('Policy checker returned an inconsistent ALLOW')
        return report
    except (OSError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
        return {'decision':'ERROR', 'error':str(error), 'checks':'Policy check could not complete'}


def main():
    report = check()
    with tempfile.NamedTemporaryFile(mode='w', prefix='handoffguard-policy-', suffix='.json',
                                     dir=os.environ.get('RUNNER_TEMP'), delete=False) as output:
        json.dump(report, output, indent=2)
        report_path = output.name
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'],'a') as output:
            output.write(f'decision={report["decision"]}\nreport={report_path}\n')
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'],'a') as summary:
            summary.write(f'## HandoffGuard: {report["decision"]}\n\n')
            summary.write('Checks delegated policy content; signatures are checked only when both public keys are supplied.\n\n')
            summary.write('<pre>' + html.escape(json.dumps(report, indent=2)) + '</pre>\n')
    print('HandoffGuard decision: ' + report['decision'])
    if report['decision'] != 'ALLOW':
        print('::error title=HandoffGuard policy check::Delegation denied or policy check could not complete. See the job summary.')
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
