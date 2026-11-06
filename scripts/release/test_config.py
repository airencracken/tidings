#!/usr/bin/env python3
"""Release metadata, documentation, service and publication contracts."""
from pathlib import Path
import re
import json
import os
import shlex
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT.joinpath('go.mod').read_text().splitlines()[0].split('/')[-1]


class ReleaseTests(unittest.TestCase):
    def test_release_version_and_operational_documents(self):
        version = (ROOT/'VERSION').read_text().strip()
        self.assertRegex(version, r'^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$')
        self.assertIn('var version = "'+version+'"', (ROOT/'cmd'/APP/'main.go').read_text())
        readme = (ROOT/'README.md').read_text()
        for name in ('releases','deployment','containers','automatic-backups','release-notes'):
            self.assertTrue((ROOT/'docs'/f'{name}.md').is_file())
            self.assertIn('docs/'+name+'.md', readme)
        self.assertIn('https://ko-fi.com/airencracken', readme)
        self.assertEqual((ROOT/'.github/FUNDING.yml').read_text().strip(), 'ko_fi: airencracken')

    def test_native_service_and_private_storage_contracts(self):
        unit=(ROOT/'deploy/systemd'/f'{APP}.service').read_text()
        for required in ('User='+APP, 'Group='+APP, 'StateDirectoryMode=0700', 'UMask=0077', 'NoNewPrivileges=true', 'ExecStart=/usr/local/bin/'+APP+' serve'):
            self.assertIn(required,unit)
        conf=(ROOT/'deploy/openrc'/f'{APP}.conf').read_text()
        self.assertIn(APP.upper()+'_DATA_DIR=/var/lib/'+APP,conf)
        self.assertIn('127.0.0.1:'+('8084' if APP=='folkweave' else '8085'),conf)
        self.assertIn('checkpath --directory --mode 0700',(ROOT/'deploy/openrc'/APP).read_text())
        for script in (ROOT/'deploy/debian').iterdir():
            self.assertNotIn('set -e',script.read_text())
            self.assertNotIn('rm -rf',script.read_text())
        self.assertNotIn(' enable ',(ROOT/'deploy/debian/postinst').read_text())

    def test_openrc_exports_ordinary_configuration_assignments(self):
        prefix = APP.upper() + '_'
        main = (ROOT/'cmd'/APP/'main.go').read_text()
        declaration = main.split('var serviceSettings = []string{', 1)[1].split('}', 1)[0]
        keys = re.findall(r'"(' + prefix + r'[A-Z_]+)"', declaration)
        keys.append(prefix + 'DATA_DIR')
        expected = {key: 'value with spaces; $literal ' + str(index) for index, key in enumerate(keys)}
        assignments = '\n'.join(key + '=' + shlex.quote(value) for key, value in expected.items())
        probe = 'import json, os; print(json.dumps({k:v for k,v in os.environ.items() if k.startswith(' + repr(prefix) + ')}))'
        env = {key:value for key,value in os.environ.items() if not key.startswith(prefix)}
        for shell in ('sh', 'bash'):
            with self.subTest(shell=shell):
                command = assignments + '\n. ' + shlex.quote(str(ROOT/'deploy/openrc'/APP))
                command += '\n' + shlex.join([sys.executable, '-c', probe])
                result = subprocess.run([shell, '-c', command], env=env, text=True, capture_output=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout), expected)

    def test_source_and_checksums_and_publication_gates(self):
        config=(ROOT/'.goreleaser.yaml').read_text()
        for required in ('goarch: [amd64, arm64]', 'CGO_ENABLED=0', 'enabled: true', 'formats: [deb]', 'algorithm: sha256', 'THIRD_PARTY_NOTICES.txt', 'postinstall:', 'type: config'):
            self.assertIn(required,config)
        workflow=(ROOT/'.github/workflows/release.yml').read_text()
        self.assertIn('needs: [build, packages, containers]',workflow)
        self.assertIn('git merge-base --is-ancestor HEAD origin/master',workflow)
        self.assertIn('--verify-tag',workflow)
        self.assertIn('refusing to replace it',workflow)
        for path in (ROOT/'.github/workflows').glob('*.yml'):
            self.assertNotIn('set -e',path.read_text())


if __name__=='__main__':
    unittest.main(verbosity=2)
