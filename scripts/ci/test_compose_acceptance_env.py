"""Protect isolated acceptance credentials when Compose adds required tokens."""
import os
from pathlib import Path
import re
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]


class ComposeAcceptanceEnvironmentTests(unittest.TestCase):
    def test_generated_environments_supply_distinct_required_metrics_tokens(self):
        required = set(re.findall(r'\$\{([A-Z_]+_METRICS_TOKEN):\?',
                                  (ROOT / 'deploy/compose.yaml').read_text()))
        self.assertEqual(len(required), 6)
        for script in ('verify-compose.sh', 'verify-compose-observability.sh'):
            with self.subTest(script=script):
                source = (ROOT / 'scripts' / script).read_text()
                body = source.split('cat >"$ENV_FILE" <<ENV\n', 1)[1].split('\nENV\n', 1)[0]
                # Expand the real heredoc without inheriting developer credentials.
                env = dict(PATH=os.defpath, TOKEN='012345abcdef', VERSION='1.11.5',
                           REVISION='test', IMAGE_TAG='1.11.5-accept-test',
                           UPDATE_VERSION='1.11.6', ADMIN_USERNAME='admin',
                           USER_USERNAME='user', PASSWORD='test')
                result = subprocess.run(['bash', '-eu', '-c', 'cat <<ENV\n' + body + '\nENV\n'],
                                        env=env, text=True, capture_output=True, check=True)
                values = dict(line.split('=', 1) for line in result.stdout.splitlines())
                tokens = [values[key] for key in required]
                self.assertEqual(len(set(tokens)), 6)
                for token in tokens:
                    self.assertGreaterEqual(len(token), 32)
                    self.assertIn(env['TOKEN'], token)
                    self.assertNotIn(token, [value for key, value in values.items()
                                            if key not in required])


if __name__ == '__main__':
    unittest.main()
