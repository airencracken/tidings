#!/usr/bin/env python3
"""Release metadata, documentation, service and publication contracts."""
from pathlib import Path
import re
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
