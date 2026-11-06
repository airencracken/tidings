#!/usr/bin/env python3
"""Verify every downloadable artifact before packages or images are published."""
import hashlib
import json
from pathlib import Path
import tarfile

ROOT=Path(__file__).resolve().parents[2]
APP=ROOT.joinpath('go.mod').read_text().splitlines()[0].split('/')[-1]
DIST=ROOT/'dist'

def main():
    manifests=list(DIST.glob(APP+'_*_checksums.txt'))
    assert len(manifests)==1
    names=set()
    for line in manifests[0].read_text().splitlines():
        digest,name=line.split(maxsplit=1);name=name.removeprefix('*')
        assert Path(name).name==name and name not in names
        assert hashlib.sha256((DIST/name).read_bytes()).hexdigest()==digest
        names.add(name)
    assert len(names)==5, names
    for arch,machine in (('amd64',62),('arm64',183)):
        files=list(DIST.glob(f'{APP}_*_linux_{arch}.tar.gz'));assert len(files)==1
        prefix=files[0].name.removesuffix('.tar.gz')
        with tarfile.open(files[0]) as archive:
            content=archive.extractfile(prefix+'/'+APP).read()
            assert content[:6]==b'\x7fELF\x02\x01' and int.from_bytes(content[18:20],'little')==machine
            for doc in ('LICENSE','THIRD_PARTY_NOTICES.txt','README.md','docs/releases.md','docs/containers.md','docs/deployment.md','docs/automatic-backups.md','deploy/systemd/'+APP+'.service','deploy/openrc/'+APP):
                assert archive.getmember(prefix+'/'+doc).isfile(),doc
        assert len(list(DIST.glob(f'{APP}_*_{arch}.deb')))==1
    sources=list(DIST.glob(APP+'_*_source.tar.gz'));assert len(sources)==1
    prefix=sources[0].name.removesuffix('.tar.gz')
    with tarfile.open(sources[0]) as archive:
        for file in ('go.mod','go.sum','.goreleaser.yaml','.github/FUNDING.yml','README.md','VERSION'):
            assert archive.extractfile(prefix+'/'+file).read()==(ROOT/file).read_bytes(),file
    print(APP+': both static architectures, source, packages, checksums, notices and operational docs verified')

if __name__=='__main__': main()
