import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]


class DeploymentRoutingTests(unittest.TestCase):
    def setUp(self):
        self.dispatch = yaml.safe_load((ROOT / '.github/workflows/deploy.yml').read_text())
        self.target = yaml.safe_load((ROOT / '.github/workflows/deploy-target.yml').read_text())

    def route(self, scope, prefix='next', environment='dev', operation='deploy'):
        block = self.dispatch['jobs']['plan']['steps'][0]['run']
        code = block.split("python3 - <<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'outputs'
            env = dict(os.environ, PREFIX=prefix, LOGICAL_ENVIRONMENT=environment,
                       SCOPE=scope, OPERATION=operation, GITHUB_OUTPUT=str(output))
            result = subprocess.run(['python3', '-c', code], env=env, capture_output=True, text=True)
            values = dict(line.split('=', 1) for line in output.read_text().splitlines()) if output.exists() else {}
            return result.returncode, values

    def test_all_routes_shared_once_before_backend(self):
        status, values = self.route('All', environment='qa')
        self.assertEqual(status, 0)
        self.assertEqual(values, dict(shared='true', backend='true', shared_environment='next-shared', backend_environment='next-qa'))
        self.assertEqual(self.dispatch['jobs']['backend']['needs'], ['plan', 'shared'])
        self.assertIn("needs.shared.result == 'success'", self.dispatch['jobs']['backend']['if'])
        self.assertFalse(self.dispatch['concurrency']['cancel-in-progress'])

    def test_component_scopes_and_final_identity(self):
        self.assertEqual(self.route('Backend', prefix='')[1]['shared'], 'false')
        self.assertEqual(self.route('Backend', prefix='')[1]['backend_environment'], 'final-dev')
        self.assertEqual(self.route('Admin/index')[1]['backend'], 'false')

    def test_invalid_or_implicit_targets_fail(self):
        for args in [('All', 'final', 'dev'), ('All', 'next', 'shared'), ('unknown', 'next', 'dev'), ('All', 'next', 'dev;echo bad')]:
            self.assertNotEqual(self.route(*args)[0], 0)
        self.assertNotEqual(self.route('All', operation='publish')[0], 0)

    def test_ci_uses_protected_target_and_explicit_approval(self):
        job = self.target['jobs']['deploy']
        self.assertEqual(job['environment'], '${{ inputs.github_environment }}')
        command = next(s['run'] for s in job['steps'] if s.get('name') == 'Run deployment wizard with explicit CI approval')
        self.assertIn('--yes --profile ""', command)
        self.assertNotIn('--github', command)
        self.assertTrue(any(s.get('uses', '').startswith('aws-actions/configure-aws-credentials') for s in job['steps']))


if __name__ == '__main__':
    unittest.main()
