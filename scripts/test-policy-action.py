#!/usr/bin/env python3
"""Exercise the Action adapter with the real CLI, including fail-closed cases."""
import json
import os
import pathlib
import subprocess
import tempfile
import unittest

ROOT=pathlib.Path(__file__).resolve().parents[1]


class PolicyActionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory(prefix='hg-action-')
        cls.binary=pathlib.Path(cls.temp.name)/'handoffguard'
        subprocess.run(['go','build','-o',str(cls.binary),'./cmd/cli'],cwd=ROOT,check=True)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def check_policy(self, child, *, parent='examples/policy-parent.json', **extra):
        with tempfile.TemporaryDirectory(prefix='hg-action-case-') as directory:
            root=pathlib.Path(directory)
            env=dict(os.environ, HG_BINARY=str(self.binary), HG_PARENT=parent, HG_CHILD=str(child),
                HG_PARENT_KEY='', HG_CHILD_KEY='', GITHUB_WORKSPACE=str(ROOT), RUNNER_TEMP=directory,
                GITHUB_OUTPUT=str(root/'outputs'), GITHUB_STEP_SUMMARY=str(root/'summary'))
            env.update(extra)
            result=subprocess.run(['python3',str(ROOT/'scripts/policy-action.py')],env=env,capture_output=True,text=True)
            outputs=dict(line.split('=',1) for line in (root/'outputs').read_text().splitlines())
            report=json.loads(pathlib.Path(outputs['report']).read_text())
            self.assertEqual(outputs['decision'],report['decision'])
            return result,report,(root/'summary').read_text()

    def test_narrowing_passes(self):
        result,report,_=self.check_policy('examples/policy-child-allow.json')
        self.assertEqual(result.returncode,0)
        self.assertEqual(report['decision'],'ALLOW')
        self.assertIn('signatures not checked',report['checks'])

    def test_expansion_fails(self):
        result,report,_=self.check_policy('examples/policy-child-deny.json')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(report['decision'],'DENY')
        self.assertIn('ACTION_EXPANDED', {v['code'] for v in report['violations']})
        self.assertIn('APPROVAL_WEAKENED', {v['code'] for v in report['violations']})

    def test_missing_or_invalid_input_fails(self):
        for parent in ('','missing.json'):
            with self.subTest(parent=parent):
                result,report,_=self.check_policy('examples/policy-child-allow.json', parent=parent)
                self.assertNotEqual(result.returncode,0)
                self.assertEqual(report['decision'],'ERROR')

    def test_unpaired_key_fails(self):
        result,report,_=self.check_policy('examples/policy-child-allow.json', HG_PARENT_KEY='missing.pub')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(report['decision'],'ERROR')

    def test_signatures_and_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            key=root/'key'
            subprocess.run([str(self.binary),'keygen','--out',str(key)],check=True,capture_output=True)
            for name, fixture in [('parent','policy-parent.json'), ('child','policy-child-allow.json')]:
                subprocess.run([str(self.binary),'envelope','create','--in',str(ROOT/'examples'/fixture),
                                '--key',str(key)+'.priv','--out',str(root/(name+'.json'))],
                               check=True,capture_output=True)
            arguments=dict(parent=str(root/'parent.json'),HG_PARENT_KEY=str(key)+'.pub',HG_CHILD_KEY=str(key)+'.pub')
            result,report,_=self.check_policy(root/'child.json',**arguments)
            self.assertEqual(result.returncode,0)
            self.assertEqual(report['decision'],'ALLOW')
            child=json.loads((root/'child.json').read_text())
            child['purpose']='tampered purpose'
            (root/'child.json').write_text(json.dumps(child))
            result,report,_=self.check_policy(root/'child.json',**arguments)
            self.assertNotEqual(result.returncode,0)
            self.assertNotEqual(report['decision'],'ALLOW')

    def test_shell_metacharacters_are_literal(self):
        with tempfile.TemporaryDirectory() as directory:
            target=pathlib.Path(directory)/'$(touch SHOULD_NOT_EXIST).json'
            target.write_bytes((ROOT/'examples/policy-child-allow.json').read_bytes())
            result,_,_=self.check_policy(target)
            self.assertEqual(result.returncode,0)
            self.assertFalse((ROOT/'SHOULD_NOT_EXIST').exists())

    def test_summary_escapes_policy_text(self):
        with tempfile.TemporaryDirectory() as directory:
            target=pathlib.Path(directory)/'child.json'
            child=json.loads((ROOT/'examples/policy-child-allow.json').read_text())
            child['allowed_actions'].append('<script>alert(1)</script>')
            target.write_text(json.dumps(child))
            result,report,summary=self.check_policy(target)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(report['decision'],'DENY')
            self.assertNotIn('<script>', summary)


if __name__=='__main__':
    unittest.main(verbosity=2)
