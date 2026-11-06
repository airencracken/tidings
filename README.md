# Tidings

<img src="docs/images/town-crier.png" alt="Tidings’ town crier with a bell and scroll" width="144" height="152" align="right">

A quiet RSS and Atom reader for you and your people. Follow the writers and
sites you choose, read a finite page, save something worthwhile, and put it down.

Tidings is Comfyware: a self-hosted Go application with SQLite, embedded assets,
ordinary HTML forms, and no frontend build or third-party requests in the reader.
One installation serves one household or small community. Each member has their
own subscriptions, tags, feed groups, saved stories, and reading state.

<br clear="right">

[Comfyware](https://comfyware.org/) · [Project page](https://comfyware.org/tidings/) · [Help and bugs](https://github.com/airencracken/tidings/issues)

This is an early development build. There are no published release packages or container images yet.

## Try the demo

```sh
make demo
# Or choose another port:
make demo PORT=9000
```

Open **http://127.0.0.1:8083**. Sign in as **demo** (host), **freya**, or
**jules**, all with password **demo-password**. The host and Freya follow the
household table; Jules starts opted out. Try both handpicked and tag-based feed
groups, personal sources, saved stories, and independent reading state.

The demo listens only on localhost, ignores all `TIDINGS_*` settings, and uses a
fresh private temporary database. Its clearly labeled sample feeds refresh
offline; no publisher requests are made, including for newly added sources.
Press Ctrl-C to stop and remove the demo. Each run starts fresh. `make demo
PORT=0` chooses a free port and prints its address.

## Start here

Requires Go 1.26 or newer. There is no JavaScript build step or database server.

```sh
make build
./bin/tidings create-owner --username yourname
./bin/tidings serve
```

The account command asks twice for a password with terminal echo disabled.
Open **http://127.0.0.1:8083** and sign in. Your first reading table is empty.
Under **Sources**, paste a direct RSS/Atom address or follow a source from the
catalog. Choose **Refresh feeds** to collect stories. Refresh is asynchronous;
reload when you want to see the results. The running server also checks feeds
every 30 minutes, with a five-minute minimum between checks of any one source.

For scripted provisioning, `--password-file PATH` reads a private, owner-only
file. Passwords are never accepted as command-line arguments. Passwords must be
12–72 bytes; usernames are 2–40 ASCII letters, numbers, underscores, or hyphens.

## Your table and the household table

Personal subscriptions stay personal. The host can also tend **Household
sources**. Every account starts with the household table **off**, including
hosts. Members can opt in under **Your account**, and opt out at any time.
Following the household table adds its stories to the member's normal reading
table and provides a separate household shelf.

Read markers and saved stories always belong to the individual. Reading a shared
story does not mark it read for anyone else. Opening a story does not mark it
read automatically; use the explicit action when you are done. Removing a source
or leaving the household table preserves stories you set aside.

Hosts create single-use invitations under **Your account**. Invitations expire
after seven days. There is no public registration. A host can recover an account
locally, which also signs that person out on every device:

```sh
./bin/tidings set-password --username someone
```

Feed organization is separate from access. Grouping a private source never shares
that subscription or its stories. As with other self-hosted apps, the operator
who controls the server and database can access the instance's data.

## Groups of feeds for your interests

Use **Feed groups** to create either kind of interest group:

- **Chosen feeds:** explicitly select sources. The same source can belong to
  several groups, such as “AI research” and “Weekend reading”.
- **Tag-based:** choose one source tag, such as `research`. Sources join and leave
  automatically as you add and remove that tag under **Sources → Edit tags**.

Personal groups belong to their creator. Hosts can create shared household
groups using household sources and household tags. Household groups are available
for reading after opting into the household table. A personal chosen-feed group
can include household sources while the member follows them; the group never
keeps access after that member opts out. Personal tag-based groups match personal
source tags; household tag-based groups match household source tags.

Tags are case insensitive, support spaces, and are entered as a comma-separated
list. Each source can have up to 12 tags, each at most 40 characters. Groups can
overlap; read/save state follows the story across all views. The optional main
topic is an additional simple filter and sets the initial tag when none is given.

## AI, tech, and infosec

The catalog includes **37 feeds verified on November 6, 2026** using the same
public-only HTTP client and parser as the application: 15 AI, 10 tech, and 12
infosec sources. It mixes independent writers, reporting, research, and primary
sources, with vendor and paywall context in the descriptions. Nothing is
subscribed automatically. A high-volume source can bring a large backlog; you
can mark just the current page read without changing anyone else's state.

For a whole topic, import a bundle under **Sources → Import subscriptions**:

| Bundle | Feeds |
| --- | --- |
| [AI](feeds/ai.opml) | 15 |
| [Tech](feeds/tech.opml) | 10 |
| [Infosec](feeds/infosec.opml) | 12 |
| [All three](feeds/all.opml) | 37 |

Import on the **Household sources** page to add the bundle to the shared table
instead. Imports are transactional and do not clear existing subscriptions.
The catalog and bundles are checked together to prevent drift. Feed availability
can change; errors appear beside the affected source. The initial check excluded
Dan Luu's oversized feed and Rapid7's malformed XML rather than listing sources
the application could not ingest.

## Own your reading life

Under **Your account**, download subscriptions as OPML for other readers, or
download a versioned JSON record containing your subscriptions and tags,
interest groups, reading state, and text of stories you have marked. Household
sources are included when followed. No credentials or other members' personal
data are exported. Shared group definitions are also available to hosts.

OPML import is supported. Importing JSON reading-state exports is not yet
implemented. The JSON export is a portable record, not a full instance backup.

For a complete backup, **stop Tidings and copy the entire data directory**.
Restore the directory with Tidings stopped. Do not copy only a live SQLite file
while its WAL journal may be active. Backups include credentials and private
reading history; keep them private.

## Hosting

| Setting | Default | Meaning |
| --- | --- | --- |
| `TIDINGS_ADDR` | `127.0.0.1:8083` | HTTP listener |
| `TIDINGS_DATA_DIR` | `./data` | Private data directory, mode 0700 |
| `TIDINGS_BASE_URL` | unset | Public HTTPS origin for invitation links |
| `TIDINGS_SECURE_COOKIES` | automatic with base URL | Set `true` to require HTTPS cookies |

The executable serves its own CSS, JavaScript, and templates. Reading, managing
sources, and all account forms work with JavaScript disabled. JavaScript only
adds a saved light/dark/system theme preference. Dates use UTC.

The little town crier welcomes readers on the sign-in screen and first empty
reading table. The transparent illustration is embedded in the binary; its
[generation prompt](docs/town-crier-prompt.txt) is kept with the source.

A simple cream bell on Tidings green identifies browser tabs. Its
[SVG source](internal/reader/static/favicon.svg), 16/32/48-pixel ICO frames, and
180-pixel Apple touch icon are embedded and available before sign-in. See
[favicon artwork](docs/favicon.md) for regenerating the raster sizes.

Comfylib v0.1.7 supplies opaque session/invitation tokens, session-bound CSRF
tokens, confirmed terminal password prompting, and shared account-form styles.
Tidings owns the feed parser, fetching policy, SQLite schema, account policy,
and templates.

Use an HTTPS reverse proxy for household hosting. Keep the application listener
private; a loopback deployment with Caddy can use:

```caddyfile
tidings.example.org {
    reverse_proxy 127.0.0.1:8083
}
```

Run Tidings with `TIDINGS_BASE_URL=https://tidings.example.org`. This requires
secure `__Host-` cookies and prints complete invitation links. Listening beyond
loopback requires an HTTPS base URL and secure cookies. For a container,
provision a writable private `/data` volume for the container's non-root UID,
create the first owner using the same image and volume, then run `serve` behind
your HTTPS proxy. A [Dockerfile](Dockerfile) is included; the container build has
not been exercised as part of the initial local validation.

`/healthz` is an unauthenticated readiness check with no account or feed data.
Sessions expire after 30 days. Login and invitation attempts share a limit of 20
per 15 minutes per connection IP (IPv6 grouped by /64). Forwarded client headers
are deliberately not trusted; clients behind a proxy share its attempt budget.
Password recovery uses the local host command; there is no email service.

Feeds must use public HTTP(S) URLs on port 80 or 443. DNS results are checked and
the validated IP is dialed directly, including on redirects. Private networks,
local services, and cloud metadata addresses are blocked. Responses are bounded
to 4 MiB and 1,000 entries per fetch. Tidings renders publisher-supplied text as
plain text without remote images, scripts, or automatic embeds. It does not
scrape full articles beyond the supplied feed; open the original when needed.

## Development and verification

```sh
make check             # formatting, vet, race tests, deliberate mutations
make fuzz-smoke        # feed and OPML parser fuzzing
make test-browser      # after installing the browser development tools below
```

Browser tools are development-only:

```sh
npm --prefix scripts/browser ci
npm --prefix scripts/browser exec -- playwright install chromium
make test-browser
```

The browser suite creates and removes its own private database and server. It
covers real invitation/signup flows with separate browser contexts, independent
reading state, household opt-in, both feed-group types, exports, finite pages,
JavaScript-free forms, both themes, and five routes at four viewport widths.
Set `TIDINGS_SCREENSHOT_DIR=/tmp/tidings-screenshots` for screenshots.

The Go tests cover units, database and HTTP integration, schema constraints,
API/export contracts, route existence, account isolation, generated state
sequences, rollback on failed writes, invitation races, and adversarial inputs.
The mutation runner, reused from Comfylib, requires targeted tests to reject
deliberate defects while their unmodified baselines pass.

Run a live catalog check explicitly; ordinary tests need no network:

```sh
TIDINGS_LIVE_FEEDS=1 go test ./internal/reader -run '^TestLiveCatalog$' -v -count=1 -timeout=300s
```

## Support

You can [support Tidings on Ko-fi](https://ko-fi.com/airencracken).
Support helps maintain Tidings and the other [Comfyware projects](https://comfyware.org/).

Licensed under AGPL-3.0-or-later. Copyright 2026 Marcus J. Hildum.
See [LICENSE](LICENSE) and [third-party components](THIRD_PARTY.md).
