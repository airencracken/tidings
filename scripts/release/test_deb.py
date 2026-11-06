#!/usr/bin/env python3
"""Install, initialize, recover and remove the package in a throwaway Debian host."""
import argparse
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parents[2]
APP=ROOT.joinpath('go.mod').read_text().splitlines()[0].split('/')[-1]
parser=argparse.ArgumentParser();parser.add_argument('--arch',choices=['amd64','arm64'],required=True);args=parser.parse_args()
packages=list((ROOT/'dist').glob(f'{APP}_*_{args.arch}.deb'));assert len(packages)==1
subprocess.run(['docker','run','--rm','--platform','linux/'+args.arch,'--label','org.comfyware.fixture=true',
                '-v',str(packages[0])+':/release.deb:ro','-v',str(ROOT/'scripts/release')+':/tests:ro',
                'debian:trixie-slim','sh','/tests/test-deb.sh',APP],check=True)
