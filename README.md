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

Current release: **[0.4.2](https://github.com/airencracken/tidings/releases/tag/v0.4.2)**.

## Inspiration

[Miniflux](https://miniflux.app/) is an inspiration for Tidings' reading features:
full-story extraction, cleaner article links, keyboard navigation, and thoughtful
feed fetching. Its quiet, readable approach is a useful reference.

Tidings is built around SQLite and a household or small community. Simple hosting,
personal subscriptions and reading state, overlapping interest groups, and an
opt-in household table shape the application from the start.

## Install

Choose a [Linux archive or Debian package](docs/releases.md),
[Docker Compose](docs/containers.md), or the
[Comfyware Gentoo overlay](https://github.com/airencracken/comfyware#add-the-overlay).
Read the [hosting](docs/deployment.md), [backup/recovery](docs/automatic-backups.md),
and [release notes](docs/release-notes.md) before sharing an installation.
The source demos below use disposable local data.

## Try the demo

```sh
make demo
# Or choose another port:
make demo PORT=9000
```

Open **http://127.0.0.1:8085**. Sign in as **demo** (host), **freya**, or
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
Open **http://127.0.0.1:8085** and sign in. Your first reading table is empty.
Under **Sources**, paste a direct RSS/Atom address or follow a source from the
catalog. New follows fetch immediately. Fetching runs in the background;
reload when you want to see the results. The running server also checks feeds
on a schedule that adapts to each source, from five minutes to 24 hours. Quiet
sources are checked less often; active ones more often. Failed requests back off,
and publisher cache and retry timing are respected. Manual refresh has a
five-minute cooldown; the host can force an immediate check.

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
  automatically as you add and remove that tag under **Sources → Edit tags and topic**.

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

## Suggested sources

The owner chooses which categories are available under **Your account → Suggested
sources**. Suggestions begin disabled on fresh installs and upgrades. Enable news,
science, culture, food, sports, nature, AI, tech, or infosec to suit your household.
Enabling a category never subscribes anyone; disabling it leaves existing
subscriptions and reading state intact. Members can always enter their own feed
URLs, discover feeds from websites, or import OPML.

The optional catalog has 45 feeds. The eight general-interest additions were
checked on November 6, 2026 with the application's public-only client and parser.
Feeds may change or disappear; errors appear beside subscribed sources. Some
sources publish excerpts or require a subscription for the original article.

Category bundles are available in [feeds/](feeds/), including [all categories](feeds/all.opml).
Import an OPML bundle under **Sources → Import subscriptions**, or use **Household
sources** to add it to the opt-in shared table. Imports are transactional and keep
existing subscriptions, topics, and tags.

## Reading stories

Stories retain their headings, links, lists, quotations, and code formatting.
Choose **Get full story** to download and extract an article when its feed supplies
an excerpt. This copy belongs to you; other readers keep their own view. A failed
download leaves the existing story available. Full stories you mark are included
in your personal JSON export and verified instance backups.

Known tracking fields such as `utm_source`, `fbclid`, and `gclid` are removed from
article links, including links inside stories. Other query values, anchors, and
signed URLs are preserved. Feed addresses and image download addresses keep their
original query strings.

Under **Your account → Reading preferences**, you can enable keyboard shortcuts.
They are **off by default**, including after an upgrade, so browser extensions
such as Tridactyl can keep their keys. When enabled: `j`/`k` select the next or
previous story, Enter opens it, `m` toggles read state, `s` toggles set aside,
`/` focuses search, and `?` opens a shortcut guide. Shortcuts leave typing fields,
interactive controls, modifier keys, and composition alone.

Feed images remain optional under **Your account → Feed images**. When enabled,
inline article images and previews load through Tidings' private local cache.

## Owner feed controls

Under **Your account → Feed fetching**, choose **Fetch all feeds now** to fetch
all active personal and household sources immediately. This bypasses the normal
five-minute cooldown, runs in the background, and coalesces repeated requests.
Normal refresh and scheduled polling retain their cooldown. No reading state or
subscriptions are changed, and members cannot invoke the instance-wide action.


## Own your reading life

Under **Your account**, download subscriptions as OPML for other readers, or
download a versioned JSON record containing your subscriptions and tags,
interest groups, reading state, and text of stories you have marked. Household
sources are included when followed. No credentials or other members' personal
data are exported. Shared group definitions are also available to hosts.

Merge a Tidings JSON export under **Your account** (up to 16 MiB, 10,000 marked
stories, 2,000 distinct sources, and 100 personal groups). The entire merge is
transactional. Existing topics, tags, groups with the same name, and marked
reading states take precedence. Household sources, household groups, and your
household opt-in are not changed. Imported story copies stay private to your
account; publisher content already present is never overwritten by an upload.
Stories saved from a source you have since removed can also be restored. When
a publisher later supplies an imported story, its read/save markers move onto
the publisher entry and its private duplicate is removed.
Exports from older Tidings builds work when their story sources are included;
otherwise make a fresh export before migrating. Explicit group membership is
restored for sources included in your personal subscriptions.

A personal export excludes credentials and is not a full instance backup.
Verified full snapshots run daily by default, starting one minute after the
server starts. The last success and any failure appear on the owner's account
page. SQLite's online backup API holds a consistent read snapshot, enforces the
uncompressed byte cap before copying, and checks database integrity and foreign
keys before publication. Comfylib compresses, checksums, publishes, and retains
these archives in the private `backups` directory (seven copies, up to 10 GiB).
The uncompressed snapshot cap defaults to 5 GiB. Compression uses `zstd` when
installed, otherwise gzip. Restoring a zstd archive also requires `zstd`.

For a manual copy while the server is running:

```sh
tidings backup --destination /private/path/manual-tidings-backups
```

Manual archives have a separate name and are never rotated automatically. Copy
completed archive directories off the host if you need protection against disk
loss. Full backups include passwords, invitations, sessions, and all members'
private reading data; keep their directories private.

Restore a complete archive into a **new, empty** private directory:

```sh
tidings restore --archive /private/path/manual-tidings-backups/manual-tidings-... --destination /private/path/tidings-restored
```

Restore checks the compressed checksum, allowed archive members, database
checksum, schema, integrity, and foreign keys before publishing the database.
It rejects a destination in use or containing an existing instance. Check the
restored instance on a separate loopback port, then stop both servers and switch
`TIDINGS_DATA_DIR` in your service configuration to the restored directory.
Keep the original directory until you have checked the restored reader. When
using the supplied systemd unit, also update `WorkingDirectory` and
`ReadWritePaths` for a custom location. Existing databases migrate transactionally to schema 5 on
open; take a stopped copy of the entire original directory before upgrading if
you need to return to an older binary.

## Hosting

| Setting | Default | Meaning |
| --- | --- | --- |
| `TIDINGS_ADDR` | `127.0.0.1:8085` | HTTP listener |
| `TIDINGS_DATA_DIR` | `./data` | Private data directory, mode 0700 |
| `TIDINGS_BASE_URL` | unset | Public HTTPS origin for invitation links |
| `TIDINGS_SECURE_COOKIES` | automatic with base URL | Set `true` to require HTTPS cookies |
| `TIDINGS_TRUSTED_PROXIES` | none | Explicit trusted proxy IPs/CIDRs, comma-separated |
| `TIDINGS_BACKUP_INTERVAL` | `24h` | Automatic backup interval, `0` disables |
| `TIDINGS_BACKUP_KEEP` | `7` | Retained automatic copies (1–30) |
| `TIDINGS_BACKUP_MAX_BYTES` | `10737418240` | Total automatic archive cap |
| `TIDINGS_BACKUP_COPY_MAX_BYTES` | `5368709120` | Uncompressed snapshot cap |

The executable serves its own CSS, JavaScript, and templates. Reading, managing
sources, and all account forms work with JavaScript disabled. JavaScript adds live fetch status, saved theme preferences, and keyboard shortcuts
for readers who enable them. Saved themes apply before page content appears. Dates use UTC. Refresh requests
from different readers queue without being dropped; repeated requests coalesce,
and sources keep a five-minute fetch cooldown. The fetch indicator updates as work progresses and shows the next automatic check.
The reader preserves headings, links, lists, quotations, tables, and code blocks.
Feed markup is sanitised before display; inline images use the same optional local
cache as story previews.

The little town crier welcomes readers on the sign-in screen and first empty
reading table. The transparent illustration is embedded in the binary; its
[generation prompt](docs/town-crier-prompt.txt) is kept with the source.

The town crier identifies browser tabs and the app header. A close-up of the
existing mascot makes the small icon readable. Its
[SVG source](internal/reader/static/favicon.svg), 16/32/48-pixel ICO frames, and
180-pixel Apple touch icon are embedded and available before sign-in. See
[favicon artwork](docs/favicon.md) for regenerating the raster sizes.

Comfylib v0.1.8 supplies opaque session/invitation tokens, session-bound CSRF
tokens, confirmed terminal password prompting (with echo disabled before the
first Linux prompt), trusted proxy resolution, backup scheduling and archives,
service configuration resolution, administrative privilege dropping, reverse
proxy configuration generation, account-form styles, and shared browser layout
assertions. These features use the existing release; no comfylib update is
required.
Tidings owns the feed parser, fetching policy, SQLite schema, account policy,
and templates.

The supplied [systemd](deploy/systemd/tidings.service) and
[OpenRC](deploy/openrc/tidings) services run as `tidings`, use a private
`/var/lib/tidings`, and keep configuration under `/etc/tidings/tidings.env` or
`/etc/conf.d/tidings`. Create the `tidings` system user/group and its private
state directory before creating the first owner. `make install-systemd` and
`make install-openrc` install the binary and service files, preserve existing
configuration, and do not enable or start the service. `DESTDIR=/tmp/package`
can stage an installation; service files expect the default `/usr/local` prefix.

Administrative commands resolve the installed service's data directory and
settings. When invoked as root they re-run as the configured service account;
unmanaged root writes are refused. Password files must be readable by that
account. Portable installs should run all commands as their own unprivileged
user. Owners can create and revoke invitations, suspend/resume members, or
permanently remove a member after confirming their own password and the member's
username. The final active owner cannot be suspended or removed.

Use an HTTPS reverse proxy for household hosting. Generate a configuration with:

```sh
tidings proxy-config caddy --domain tidings.example.org
# nginx and apache are also supported; see proxy-config --help.
```

This command prints configuration without opening the database or modifying the
host. Validate the complete proxy configuration before loading it. Keep the
application listener private; a loopback deployment with Caddy can use:

```caddyfile
tidings.example.org {
    reverse_proxy 127.0.0.1:8085
}
```

Run Tidings with `TIDINGS_BASE_URL=https://tidings.example.org`. This requires
secure `__Host-` cookies and prints complete invitation links. Listening beyond
loopback requires an HTTPS base URL and secure cookies. For a container,
provision a writable private `/data` volume for the container's non-root UID,
create the first owner using the same image and volume, then run `serve` behind
your HTTPS proxy. The [container guide](docs/containers.md) covers the published, tested image
and its private named-volume setup.

`/healthz` is an unauthenticated readiness check with no account or feed data.
Sessions expire after 30 days. Login and invitation attempts share a limit of 20
per 15 minutes per client IP (IPv6 grouped by /64). Forwarding headers are
honored only when the connection comes from a proxy explicitly included in
`TIDINGS_TRUSTED_PROXIES`; the first untrusted hop determines the client. With
an empty trust list, clients behind a proxy share its attempt budget. A local
proxy normally uses `127.0.0.1/32,::1/128`.
Password recovery uses the local host command; there is no email service.

Feeds must use public HTTP(S) URLs on port 80 or 443. DNS results are checked and
the validated IP is dialed directly, including on redirects. Private networks,
local services, and cloud metadata addresses are blocked. Responses are bounded
to 4 MiB and 1,000 entries per fetch. Tidings renders publisher-supplied text as
plain text. Hosts may enable feed-supplied images, which are fetched and cached by Tidings. It does not
scrape full articles beyond the supplied feed; open the original when needed.

## Development and verification

```sh
make check             # formatting, vet, race tests, deliberate mutations
make fuzz-smoke        # feed, OPML, and JSON restore parser fuzzing
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

## Common setup

New source builds share [browser and command-line setup](docs/setup.md) with the
other Comfyware apps: `tidings setup` authorizes the first-owner browser page,
and `tidings create-owner --username NAME` prompts privately by default.

## Feed images and fetch progress

The reading table and Sources show live fetch status, completed checks, failures,
and the next automatic check. Following a source or importing OPML starts its
first fetch immediately. A completed fetch offers a link to the updated reading
table, so the page does not move while you read. Without JavaScript, reload to
see the current status.

Owners can enable **Your account → Feed images → Show feed images**. This is off
by default, including on upgrade. Readers receive previews from authenticated
Tidings routes; their browsers do not contact image publishers. The fetcher uses
the same validated public-network transport as RSS, with a 5 MiB download cap,
12-million-pixel decode cap, and previews up to 1280 pixels. JPEG, PNG, GIF, and
WebP inputs become static JPEG previews. Concurrent requests share one download.

The private `cache/images` directory has a 100 MiB/2,048-file retention cap and a
seven-day lifetime. Expired previews are fetched again on access; old files are
pruned on writes. Cached images are derived data and are excluded from database
backups. Image settings and source image addresses are preserved in backups.

Check the bundled sources with the production fetcher before a release:

```sh
TIDINGS_LIVE_FETCH_TEST=1 go test ./internal/reader -run TestLiveCatalogFetch -v -parallel 4
```
