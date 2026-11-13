# Tidings releases

Each release lists what members and owners will notice, then notes for the
person who runs the server. Back up the stopped data directory before any
upgrade, and keep that copy if you might return to an older version.

## Unreleased

- Tidings no longer records who signs in or which stories anyone opens.
  Instance metrics cover operational health: members, sources, stories, source
  errors, database size and backups.
- Fetch progress appears on **Sources**, and on the reading table only while a
  fetch you asked for is running or has just finished. Failed sources show their
  last error in the source list.
- Search and the Topic and Source filters sit behind **Filter** on the reading
  table. It opens by itself when a filter is active.
- **Your account** holds personal settings only. Invitations, members, household
  sources, suggested sources, feed images and backups are under **Admin**.
- The article page has one main button, **Mark read and return**. **Read on**
  the publisher's site is a plain link.
- Text is at least 12 pixels, and buttons and links are larger. Sign-in, join,
  invitations and metrics use Tidings' headings and colours.
- Expired forms and too many sign-in attempts show a normal Tidings page.

Operators:

- Opening the database deletes all sign-in and story-open rows stored by 0.7.0
  and 0.8.0, including in databases restored from older backups. The schema
  version stays at 5.

## 0.8.0

- Choose your timezone under **Your account → Times and dates**. UTC is the
  default. **Use this device's timezone** fills the field when JavaScript is on;
  nothing is saved until you choose **Save timezone**.
- Dates on pages follow your choice. The metrics page keeps its reporting days
  in UTC.

Operators:

- Requires Comfylib 0.6.0. Startup adds a separate table for display
  preferences. The database schema and export format are unchanged.
- Stored timestamps and API responses stay in UTC.

## 0.7.0

- The owner metrics page uses the dashboard shared by the Comfyware apps.
- Only owners can open it. It showed seven days of sign-ins and story opens per
  account. (Removed in the next release.)

Operators:

- Requires Comfylib 0.5.0, which adds its own versioned tables. The Tidings
  schema is unchanged and existing backups remain supported.
- Activity was kept for 90 days and deleted with its account. No IP addresses
  or browser identifiers were recorded.

## 0.6.0

- Owners get an instance metrics page with member, source, story and storage
  totals, plus backup status and configured Comfyware connections. It works
  without JavaScript.

Operators:

- Sign-in rate limits use the shared Comfylib implementation with the same
  budget as before.
- No database migration.

## 0.5.4

- Owners get an **Admin** link in the main navigation and an Admin page for
  household sources, members and backup status.

Operators:

- Uses Comfylib 0.3.0 for suite helpers, styles and browser controls, and the
  separate Comfylib OIDC 0.1.0 module for community sign-in.
- No database migration.

## 0.5.1

Operators:

- OpenRC and Docker Compose now pass the optional app navigation and community
  sign-in settings to the service. Both stay off by default.

## 0.5.0

- An optional **Apps** menu links to your other Comfyware apps.
- Optional community sign-in with OpenID Connect. You link it to an existing
  account yourself; accounts are never created or matched by email address.
  Local passwords keep working.
- Choose comfortable or compact story spacing.
- Older and newer page links stay stable while new stories arrive.

Operators:

- Requires Go 1.26.11 or newer to build, and Comfylib 0.2.0.
- Configuring community sign-in adds a table for identity links. See
  [Comfyware connections](suite.md).
- Updates golang.org/x/crypto to 0.59.0 and golang.org/x/net. Refuses dangling
  configuration symlinks.

## 0.4.2

Operators:

- Shipped service configuration uses `KEY="value"` for every setting. Existing
  files keep working and are preserved on upgrade.
- No database migration.

## 0.4.1

- The town crier replaces the bell in browser tabs and the header.
- **Get full story** works with more publishers, including some that refused
  the previous TLS settings or returned HTTP 406.
- Download errors say what went wrong: an HTTP error, a timeout, a size limit or
  an extraction failure. The existing story stays readable.

Operators:

- Server logs record failure details without query strings or response bodies.
- No database migration.

## 0.4.0

- Stories keep their headings, links, lists, quotations, tables and code.
- **Get full story** downloads the article from the publisher. Your copy is
  yours and travels with your export.
- Known tracking fields such as `utm_source` are removed from article links.
- Optional keyboard shortcuts, off by default.
- Sources are checked between every five minutes and once a day, depending on
  how often they publish.
- A saved theme applies before the page appears, so pages no longer flash.

Operators:

- Migrates to schema 5. Each feed is fetched in full once after the upgrade;
  reading state is kept.

## 0.3.1

- Turning on feed images fills in images for stories fetched by older versions.

## 0.3.0

- Set up the first owner in the browser with `tidings setup`.
- New and imported sources fetch right away, with live progress.
- Owners can turn on feed images. Tidings fetches and caches them, so readers'
  browsers never contact the publisher.
- Roomier story cards.

Operators:

- `create-owner` and `set-password` prompt twice with hidden input by default,
  and accept `--password-stdin`, `--password-file` and `--data-dir`. Changing a
  password signs out that account's other sessions.
- Migrates to schema 4 with feed images off.

## 0.2.0

- Invitations have their own page. Links use the configured public address and
  are shown once.
- Owners can choose **Fetch all feeds now** to check every source immediately.
- Owners choose which suggested-source categories members see. All start off.
  News, science, culture, food, sports and nature join AI, tech and infosec.

Operators:

- Fixes OpenRC passing plain assignments to the service.
- Migrates to schema 3. Schema 2 backups still restore; restore one to return to
  0.1.0.

## 0.1.0

The first public release.

- RSS and Atom sources, tags, feed groups and an optional household table.
- Personal reading state, saved stories, OPML import and export, and JSON
  export and import.
- Automatic and manual verified backups, restore, and account recovery.

Operators:

- Linux amd64 and arm64 archives, Debian packages, container images and a
  Gentoo overlay. The native port is 8085.

[Installation](releases.md) · [Containers](containers.md) · [Hosting](deployment.md) · [Backups](automatic-backups.md)
