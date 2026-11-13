# Third-party components

Exact versions of all Go dependencies are in `go.mod` and `go.sum`. The main
runtime dependencies are:

| Component | Use | License |
| --- | --- | --- |
| Comfylib | Tokens, passwords, backups, proxy and service tools, shared pages | AGPL-3.0-or-later |
| Go x/crypto | bcrypt passwords | BSD-3-Clause |
| Go x/net | HTML parsing and cleaning, XML character sets | BSD-3-Clause |
| Readeck go-readability v2 | Full-story extraction | MIT |
| Go x/image | Feed image decoding and resizing | BSD-3-Clause |
| Gofrs flock | Instance and command file locks | BSD-3-Clause |
| Modernc SQLite | SQLite without cgo | BSD-3-Clause; SQLite public domain |

Playwright (Apache-2.0) is a development dependency for browser checks, pinned
in `scripts/browser/package-lock.json`. Nothing from it is served to readers.

[Miniflux](https://miniflux.app/) inspired Tidings' article reading, keyboard
navigation, link cleaning and adaptive fetching.

Publisher names and feed addresses belong to their publishers; no affiliation is
implied. Story text remains the publisher's work.
