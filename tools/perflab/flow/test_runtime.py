import json
from pathlib import Path
import sys
import tempfile
import unittest
from flow.config import load
from flow.runtime import Run, lock, stop, identity


class RuntimeTests(unittest.TestCase):
    def test_lock_and_unfinished(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with lock(root):
                with self.assertRaises(ValueError), lock(root):
                    pass
                run = Run(load(), root)
            with self.assertRaises(ValueError):
                Run(load(), root)
            stop(root)
            self.assertFalse(json.loads((run.directory / 'state.json').read_text())['execution_completed'])
            Run(load(), root).finish(2)

    def test_process_cleanup_and_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Run(load(), Path(tmp))
            run.update('checking')
            run.update('running', stage='low')
            child = run.processes.start([sys.executable, '-c', 'import time; time.sleep(60)'])
            self.assertIsNotNone(identity(child.pid))
            run.finish(130, 'manual_stop')
            self.assertIsNotNone(child.poll())
            self.assertFalse(run.state['execution_completed'])
            self.assertEqual(run.state['status'], 'completed')
            self.assertEqual(run.state['collection'], 'closed')
