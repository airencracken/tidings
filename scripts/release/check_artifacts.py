#!/usr/bin/env python3
"""Run the published shared release contract against this application."""
from pathlib import Path
import subprocess
import sys
ROOT=Path(__file__).resolve().parents[2]
raise SystemExit(subprocess.call([sys.executable,str(ROOT/'scripts/comfy.py'),'check-artifacts',*sys.argv[1:]],cwd=ROOT))
