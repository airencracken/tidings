# Tidings

<img src="docs/images/town-crier.png" alt="Tidings’ town crier with a bell and scroll" width="144" height="152" align="right">

A quiet RSS and Atom reader for you and your people. Follow the writers and
sites you choose, read a finite page, set something aside, and put it down.

Tidings is part of Comfyware. It is one Go program with a SQLite database. One
installation serves a household. Each member has their own subscriptions, tags,
feed groups, saved stories and reading state.

<br clear="right">

[Comfyware](https://comfyware.org/) · [Project page](https://comfyware.org/tidings/) · [Help and bugs](https://github.com/airencracken/tidings/issues)

Current release: **[0.8.0](https://github.com/airencracken/tidings/releases/tag/v0.8.0)** · [What's new](docs/release-notes.md)

## Install

Choose a [Linux archive or Debian package](docs/releases.md),
[Docker Compose](docs/containers.md), or the
[Comfyware Gentoo overlay](https://github.com/airencracken/comfyware#add-the-overlay).
Then read the [hosting guide](docs/deployment.md) and
[backups and recovery](docs/automatic-backups.md) before you invite anyone.

## Try the demo

```sh
make demo            # or: make demo PORT=9000
```

Open **http://127.0.0.1:8085** and sign in as **demo** (the owner), **freya** or
**jules**. The password for all three is **demo-password**. The demo uses
offline sample feeds and a temporary database, and ignores `TIDINGS_*`
settings. Press Ctrl-C to stop it and delete everything.

## Build from source

You need Go 1.26.11 or newer. There is no JavaScript build step and no database
server.

```sh
make build
./bin/tidings create-owner --username yourname
./bin/tidings serve
```

Open **http://127.0.0.1:8085** and sign in. To set up the first owner in the
browser instead, see [setup](docs/setup.md).

## Reading

Add sources under **Sources**: paste an RSS or Atom address, find a feed from a
website address, choose from the suggested sources, or import an OPML file.
New sources fetch right away. After that, Tidings checks each source on its own
schedule, from every five minutes to once a day, depending on how often it
publishes. **Refresh feeds** checks your sources now, skipping any checked in the
last five minutes.

The reading table shows a finite page of stories, newest first. There are no
unread counts. **Filter** holds search and the Topic and Source filters.
Opening a story does not mark it read; choose **Mark read** or **Mark read and
return** when you are done. **Set aside** keeps a story on its own shelf.

Stories keep their headings, links, lists, quotations, tables and code. Tidings
cleans the publisher's markup before showing it and removes known tracking
fields such as `utm_source` from links. When a feed has only an excerpt, **Get
full story** downloads the article and keeps a private copy for you.

Keyboard shortcuts are off by default. Turn them on under **Your account →
Reading preferences**, where the shortcut guide lives. Leave them off if a
browser extension already uses these keys.

Choose your timezone under **Your account → Times and dates**. UTC is the
default.

## Feed groups and tags

Each source has a topic and up to 12 tags. Tags are case-insensitive, may
contain spaces, and are at most 40 characters. Edit them under **Sources → Edit
tags and topic**.

**Feed groups** gather sources around an interest, in one of two ways:

- **Chosen feeds:** you pick the sources. A source can be in several groups.
- **Tag-based:** every source with a given tag joins, and leaves when you remove
  the tag.

Groups only organise. They never share a source or its stories with anyone.

## The household table

The owner can keep a set of **Household sources**. Every account, including the
owner's, starts with the household table off. Turn it on under **Your account**
to add those stories to your reading table and a Household shelf. Read and saved
markers stay yours. Stories you set aside are kept when you stop following.

## Your data

Under **Your account** you can download your subscriptions as OPML, or all your
reading data as JSON: subscriptions, tags, groups, reading state, and the text
of stories you marked. You can merge a Tidings JSON export from another
instance, up to 16 MB, 10,000 marked stories, 2,000 sources and 100 groups.
Your existing choices win when they conflict. Exports never include passwords
or other members' data.

Whoever runs the server can read everything in its database.

## For owners

Owners have an **Admin** link in the main navigation:

- **Invitations:** each link admits one person and expires after seven days.
  There is no public sign-up.
- **Household members:** suspend, resume or remove a member. At least one active
  owner must remain. To reset a forgotten password, run
  `tidings set-password --username NAME` on the server. There is no email.
- **Household sources:** the shared sources behind the household table. Create
  household feed groups under **Feed groups**.
- **Suggested sources:** choose which of the 45 catalog feeds members can browse,
  by category. All categories start off. Enabling one never subscribes anyone.
  The same feeds are in [feeds/](feeds/) as OPML.
- **Feed images:** off by default. When on, Tidings fetches and caches pictures
  from feeds, so readers' browsers never contact publishers.
- **Feed fetching:** **Fetch all feeds now** checks every source in the
  household immediately, including ones not yet due.
- **Instance metrics:** member, source and story totals, sources with errors,
  database size and backup status. Tidings does not record who signs in or what
  anyone reads.

## Hosting

| Setting | Default | Meaning |
| --- | --- | --- |
| `TIDINGS_ADDR` | `127.0.0.1:8085` | HTTP listener |
| `TIDINGS_DATA_DIR` | `./data` | Private data directory |
| `TIDINGS_BASE_URL` | unset | Public HTTPS address, used in invitation links |
| `TIDINGS_SECURE_COOKIES` | on with an HTTPS base URL | Set `true` to require HTTPS cookies |
| `TIDINGS_TRUSTED_PROXIES` | none | Proxy IPs or CIDRs whose forwarding headers to trust |
| `TIDINGS_BACKUP_INTERVAL` | `24h` | Automatic backup interval; `0` turns backups off |
| `TIDINGS_BACKUP_KEEP` | `7` | Automatic backups to keep (1–30) |
| `TIDINGS_BACKUP_MAX_BYTES` | `10737418240` | Total size cap for automatic backups |
| `TIDINGS_BACKUP_COPY_MAX_BYTES` | `5368709120` | Size cap for one uncompressed snapshot |

Put Tidings behind an HTTPS reverse proxy. `tidings proxy-config caddy --domain
tidings.example.org` prints a starting configuration; `nginx` and `apache` also
work. Listening on anything other than loopback requires an HTTPS
`TIDINGS_BASE_URL`. The [hosting guide](docs/deployment.md) covers service
accounts, systemd and OpenRC, and logs. Optional app navigation and community
sign-in are in [Comfyware connections](docs/suite.md).

Things to know:

- Sessions last 30 days. Sign-in and join attempts are limited to 20 per 15
  minutes per client address.
- Tidings fetches only public HTTP(S) addresses on ports 80 and 443. Private
  networks and cloud metadata addresses are blocked, including after redirects.
  A feed fetch reads at most 4 MB and 1,000 entries.
- `/healthz` answers without signing in and shows no data.
- Upgrades migrate the database on startup. Back up first; an older version
  cannot open a migrated database.

## Development

```sh
make check          # formatting, vet, race tests and mutation tests
make fuzz-smoke     # parser fuzzing
npm --prefix scripts/browser ci
npm --prefix scripts/browser exec -- playwright install chromium
make test-browser   # browser checks
```

Set `TIDINGS_SCREENSHOT_DIR` to save screenshots from the browser checks.
Checks against real publishers are opt-in:

```sh
TIDINGS_LIVE_FEEDS=1 go test ./internal/reader -run '^TestLiveCatalog$' -v -count=1 -timeout=300s
TIDINGS_LIVE_FETCH_TEST=1 go test ./internal/reader -run TestLiveCatalogFetch -v -parallel 4
```

[Miniflux](https://miniflux.app/) inspired full-story extraction, link cleaning,
keyboard navigation and adaptive fetching. The town crier's
[generation prompt](docs/town-crier-prompt.txt) and [favicon notes](docs/favicon.md)
live in `docs/`.

## Support

You can [support Tidings on Ko-fi](https://ko-fi.com/airencracken). It helps
maintain Tidings and the other [Comfyware projects](https://comfyware.org/).

## License

Licensed under AGPL-3.0-or-later. Copyright 2026 Marcus J. Hildum.
See [LICENSE](LICENSE) and [third-party components](THIRD_PARTY.md).
