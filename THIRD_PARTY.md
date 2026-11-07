# Third-party components

Tidings uses Comfylib v0.1.8 for opaque tokens, session-bound CSRF tokens,
password confirmation and hidden input, proxy client resolution, backup scheduling,
service configuration, administrative privilege dropping, proxy examples, and
shared account-form styles and browser checks,
under AGPL-3.0-or-later. `scripts/mutate.py` is copied from Comfylib's mutation
runner under the same license. [Miniflux](https://miniflux.app/) is a design inspiration for article reading,
keyboard navigation, link cleanup, and adaptive feed fetching.
The interface follows the Comfyware family palette
and server-rendered form conventions used by Witmoot.

Go dependencies and their transitive versions are recorded in `go.mod` and
`go.sum`. Principal runtime dependencies are:

| Component | Use | License |
| --- | --- | --- |
| Comfylib | Tokens, passwords, backups, proxy/service tools, shared forms | AGPL-3.0-or-later |
| Go x/crypto | bcrypt passwords | BSD-3-Clause |
| Go x/net | HTML parsing, sanitising, and XML charset support | BSD-3-Clause |
| Readeck go-readability v2 | Local full-story extraction | MIT |
| Go x/image | Cached image decoding and resizing | BSD-3-Clause |
| Go x/term | Hidden terminal password input | BSD-3-Clause |
| Gofrs flock | Instance and administrative file locks | BSD-3-Clause |
| Modernc SQLite | SQLite without cgo | BSD-3-Clause; SQLite public domain |

Playwright is an Apache-2.0 development dependency, pinned in
`scripts/browser/package-lock.json`. No frontend package is served to readers.
Publisher names and feed addresses identify their respective publishers; no
affiliation is implied. Story text remains the publisher's material.
