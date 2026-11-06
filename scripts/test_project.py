#!/usr/bin/env python3
"""Check the public repository introduction and GitHub sponsorship metadata."""
from html.parser import HTMLParser
from pathlib import Path
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
