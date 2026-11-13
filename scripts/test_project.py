#!/usr/bin/env python3
"""Check the public repository introduction and GitHub sponsorship metadata."""
from html.parser import HTMLParser
from pathlib import Path
import re
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NAME = ROOT.joinpath("go.mod").read_text().splitlines()[0].split("/")[-1]


class Images(HTMLParser):
    def __init__(self, text):
        super().__init__()
        self.images = []
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        if tag == "img":
            self.images.append(dict(attrs))


def errors(root, name):
    problems = []
    text = (root / "README.md").read_text()
    funding = root / ".github/FUNDING.yml"
    if not funding.is_file() or funding.read_text().strip() != "ko_fi: airencracken":
        problems.append("GitHub sponsorship must use the existing Ko-fi account")
    for link in ("https://comfyware.org/", "https://comfyware.org/" + name + "/",
                 "https://github.com/airencracken/" + name + "/issues", "https://ko-fi.com/airencracken"):
        if "](" + link + ")" not in text:
            problems.append("Missing public project link: " + link)
    problems += release_errors(root, name, text)
    images = Images(text).images
    if len(images) != 1:
        problems.append("Expected one introductory mascot")
    for image in images:
        path = root / image.get("src", "")
        if (not path.resolve().is_relative_to(root.resolve()) or not path.is_file()
                or path.stat().st_size > 100000):
            problems.append("Mascot must be a small local file inside the repository")
        if not image.get("alt") or image.get("width") != "144":
            problems.append("Mascot needs descriptive text and a bounded display size")
    return problems


def release_errors(root, name, text):
    """The README names the current release once and links to its notes."""
    problems = []
    version = (root / "VERSION").read_text().strip() if (root / "VERSION").is_file() else ""
    tag = "https://github.com/airencracken/" + name + "/releases/tag/v" + version
    lines = re.findall(r"^Current release: .*$", text, re.M)
    if lines != ["Current release: **[" + version + "](" + tag + ")** · [What's new](docs/release-notes.md)"]:
        problems.append("README must have one current release line matching VERSION " + version)
    if re.search(r"^#+ .*\b\d+\.\d+\.\d+\b", text, re.M):
        problems.append("Release notes belong in docs/release-notes.md, not README headings")
    headings = re.findall(r"^## (.+)$", text, re.M)
    if headings[-2:] != ["Support", "License"]:
        problems.append("README must end with Support and License")
    return problems


class ProjectTests(unittest.TestCase):
    def test_public_project_contract(self):
        self.assertEqual(errors(ROOT, NAME), [])

    def test_broken_funding_links_and_unsafe_artwork_are_rejected(self):
        original = (ROOT / "README.md").read_text()
        image = Images(original).images[0]["src"]
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            (fixture / ".github").mkdir()
            funding = fixture / ".github/FUNDING.yml"
            funding.write_text("ko_fi: airencracken\n")
            (fixture / "VERSION").write_text((ROOT / "VERSION").read_text())
            asset = fixture / image
            asset.parent.mkdir(parents=True)
            asset.write_bytes((ROOT / image).read_bytes())
            for before, after in (("https://ko-fi.com/airencracken", "https://example.org/"),
                                  ("https://comfyware.org/" + NAME + "/", "https://example.org/"),
                                  (image, "../../private.png"), (image, "https://example.org/mascot.png"),
                                  ('width="144"', 'width="4096"')):
                with self.subTest(after=after):
                    self.assertIn(before, original)
                    (fixture / "README.md").write_text(original.replace(before, after))
                    self.assertTrue(errors(fixture, NAME))
            (fixture / "README.md").write_text(original)
            self.assertEqual(errors(fixture, NAME), [])
            funding.write_text("ko_fi: wrong-account\n")
            self.assertTrue(errors(fixture, NAME))

    def test_release_line_must_match_version_and_notes_stay_out(self):
        original = (ROOT / "README.md").read_text()
        version = (ROOT / "VERSION").read_text().strip()
        self.assertEqual(release_errors(ROOT, NAME, original), [])
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            for readme, recorded in (
                    (original, "0.0.1"),
                    (original, ""),
                    (original.replace("Current release:", "Latest:"), version),
                    (original + "\nCurrent release: **[9.9.9](x)**\n", version),
                    (original.replace("## Install", "## Tidings 9.9.9\n\nNotes.\n\n## Install"), version),
                    (original + "\n## Common setup\n\nAppended after the license.\n", version),
                    (original.replace("## Support", "## Sponsor"), version)):
                with self.subTest(recorded=recorded, size=len(readme)):
                    (fixture / "VERSION").write_text(recorded + "\n")
                    self.assertTrue(release_errors(fixture, NAME, readme))


if __name__ == "__main__":
    unittest.main(verbosity=2)
