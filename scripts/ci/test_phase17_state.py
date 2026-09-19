"""Direct regressions for candidate binding and extraction cleanup boundaries."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import candidate_runtime
import verify_phase17_state as state


class StateTests(unittest.TestCase):
    def test_manifest_binding_rejects_reuse_and_public_work(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            source=root/'source.json'; source.write_text('source')
            target=root/'target.json'; target.write_text('target')
            args=SimpleNamespace(from_manifest=source,manifest=target,work=root/'work')
            def bundle(path):
                return {'version':'1.13.6' if path==source else '1.14.4'}
            with patch.object(state,'verify_bundle',side_effect=bundle):
                self.assertEqual(state.inputs(args)[3],state.inputs(args)[3])
                target.write_text('different candidate')
                with self.assertRaisesRegex(ValueError,'other manifests'):state.inputs(args)
                args.work.chmod(0o755)
                with self.assertRaisesRegex(ValueError,'private'):state.inputs(args)

    def test_extraction_refuses_foreign_container_cleanup(self):
        manifest={'version':'1.14.4','revision':'a'*40,'images':{'marshaller':{
            'ref':'example/marshaller@sha256:'+'b'*64,'platforms':{'linux/amd64':'sha256:'+'c'*64}}}}
        calls=[]
        def docker(*args):
            calls.append(args)
            if args[0]=='create':return 'container'
            if args[0]=='cp':Path(args[-1]).write_bytes(b'binary')
            if args[0]=='inspect':return json.dumps([{'Config':{'Labels':{'io.gopulse.candidate-extraction':'foreign'}}}])
            return ''
        with tempfile.TemporaryDirectory() as directory, patch.object(candidate_runtime,'verify_bundle',return_value=manifest), patch.object(candidate_runtime,'run',side_effect=docker):
            with self.assertRaisesRegex(RuntimeError,'ownership'):
                candidate_runtime.extract(Path('manifest'),Path(directory),['marshaller'])
        self.assertFalse(any(call[0]=='rm' for call in calls))


if __name__=='__main__':unittest.main()
