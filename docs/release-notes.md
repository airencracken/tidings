# Tidings releases

Each release lists what members and owners will notice, then notes for the
person who runs the server. Back up the stopped data directory before any
upgrade, and keep that copy if you might return to an older version.

## 0.9.0

- **Save for later** replaces **Set aside**.
- New: **Skip** a story that is not for you. It leaves Unread, All stories and
  group shelves without being marked read or saved. Undo it under **Skipped
  stories**, below the shelves. Saving a skipped story brings it back. The
  keyboard shortcut is `x`.
- Topics are gone; tags are how you organise sources. Each source's topic is
  now its first tag, and the first tag shows on each story. Tags keep the order
  you type. Old links to a Topic filter open the matching Tag filter.
- Search and the Tag and Source filters sit behind **Filter**, which opens by
  itself when a filter is on.
- Fetch progress shows on **Sources**, and on the reading table only while your
  own refresh runs. Failed sources show their error in the source list.
- The article page has one main button, **Mark read and return**, plus **Save
  for later** and **Skip**.
- **Your account** holds only your own settings. Invitations, members and the
  other owner tools are under **Admin**.
- Pick System, Light or Dark and a colour palette from the Account menu.
  Owners choose the site palette under **Admin → Site colours**; Sage is the
  default.
- Tidings no longer records who signs in or which stories anyone opens.
  Instance metrics show site health instead: requests, errors and response
  times per kind of page, plus failed sign-ins, rate-limited attempts and
  rejected invitation links.
- Larger text and buttons, a Copy button for invitation links, and plain error
  pages.

Operators:

- Migrates to schema 7. It adds the skip marker and tag order, and turns each
  personal and household topic into the source's first tag. Topic columns stay
  but are no longer read. Backups from schema 2 to 6 still restore.
- A topic is never dropped: a source that had 12 tags keeps all 13. Saving its
  tags again asks the member to remove one.
- A tag-based feed group gains a source whose old topic matches the group's tag.
- Old exports and OPML folders import as first tags. New exports set
  `category` to the first tag, and include `"skipped": true` only for skipped
  stories, so earlier versions still import them.
- Upgrading deletes the per-account sign-in and story-open records kept by
  0.7.0 and 0.8.0, including in restored backups.
- Requires Comfylib 0.7.0, which provides site-health metrics, the shared
  colour tokens, palettes and theme picker, error pages, and the password rule
  (12 or more characters, at most 72 bytes).
- Saved theme choices carry over. The owner's palette is stored in a new
  `site_palette` table.

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
