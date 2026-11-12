# Tidings 0.7.0

Native admin metrics now use one shared dashboard with identical typography, spacing, cards, tables and reporting definitions across the apps. Successful local and community sign-ins and authorized content opens are recorded by Comfylib 0.5.0. Views count once per account, item and UTC day; thumbnails, background assets, failed requests and signed-out visits do not count.

Only active administrators (called owners in this app where applicable) can read the dashboard or its account sign-in summaries. Ordinary members, guests and moderators cannot read analytics. The dashboard shows the past seven UTC days, when collection began, daily totals, recent sign-ins by account, and most-viewed content. Recording begins after upgrade; no earlier history is inferred. No IP addresses or browser identifiers are recorded. Activity is retained for ninety UTC days, pruned at startup and daily while serving, and removed when its account is deleted.

The analytics tables are additive and separately versioned by Comfylib; existing application data and application migration versions are unchanged. Back up before upgrading. Existing archives remain supported. Shared browser contracts now check exact dashboard styling, widths and access boundaries in both themes and with JavaScript disabled.

# Tidings 0.6.0

Native owner/admin metrics now show aggregate membership, content, storage and recent activity, with owner-only operational summaries for backups and configured suite connections. The pages work without JavaScript and follow the application's existing permissions.

Rate limiting uses the bounded shared implementations while retaining the app's own budgets. Release preparation, artifact verification, browser fixtures and immutable CI workflows use the published Comfylib pin directly. A release preflight verifies version metadata, cold module caches and the exact shared workflow revision before publication.

No application database migration is added. Existing service configuration and account permissions remain supported.

# Tidings releases

## 0.5.4

- Make administration visible in the main navigation for owners. Add a dedicated
  Admin settings page for household sources, members and backup status.
- Resolve shared tooling on fresh container CI runners and verify it with the
  module proxy disabled before running the release gates.

- Import the suite helpers directly from published Comfylib 0.3.0 and use its
  embedded styles and browser controls.
- Replace the local OpenID Connect protocol adapter with the separately versioned
  Comfylib OIDC module 0.1.0. Existing local session and permission hooks remain
  application-owned.
- Run service configuration, deployment, release, artifact scanning, mutation and
  container checks directly from the pinned shared module. Retain application
  configuration and both existing backup/restore lifecycle profiles.
- Add integration tests that verify shared asset routes and exact published bytes.

This maintenance release adds no application database migration or new default
integration. Back up your data before upgrading. When upgrading from older
versions, follow the earlier release notes for any migrations you have not run.

## 0.5.1

- Forward optional app navigation and community sign-in settings through OpenRC
  and Docker Compose. Native templates document disabled defaults. Regression
  tests check literal values and reject missing service exports.
- Add an optional Apps menu and consistent keyboard, focus and account controls.
- Add explicitly linked community sign-in using OpenID Connect, state, nonce and
  PKCE. Existing local accounts and passwords remain available; accounts are never
  created or linked by matching email addresses. Configure this only when wanted.
- Use published Comfylib v0.2.0 and require Go 1.26.11 or newer for source builds.
- Upgrade golang.org/x/crypto to v0.59.0 and scan the actual release executables
  for vulnerable dependencies before publication.

- Add comfortable and compact spacing with preferences kept per application.
- Use stable published-time and ID cursors for efficient older/newer navigation;
  existing offset links remain supported and visibility checks are preserved.
- Upgrade golang.org/x/net and refuse unsafe dangling configuration symlinks.

Read [Suite integration](suite.md) for configuration and security boundaries.
Back up the stopped data directory before upgrading. Shared sign-in creates an
additive identity-binding table when configured. Keep the pre-upgrade backup;
use it when returning to older binaries rather than opening a migrated database
with an older application. Publishing a release does not change running services.


# Tidings 0.5.0

- Add an optional Apps menu and consistent keyboard, focus and account controls.
- Add explicitly linked community sign-in using OpenID Connect, state, nonce and
  PKCE. Existing local accounts and passwords remain available; accounts are never
  created or linked by matching email addresses. Configure this only when wanted.
- Use published Comfylib v0.2.0 and require Go 1.26.11 or newer for source builds.
- Upgrade golang.org/x/crypto to v0.59.0 and scan the actual release executables
  for vulnerable dependencies before publication.

- Add comfortable and compact spacing with preferences kept per application.
- Use stable published-time and ID cursors for efficient older/newer navigation;
  existing offset links remain supported and visibility checks are preserved.
- Upgrade golang.org/x/net and refuse unsafe dangling configuration symlinks.

Read [Suite integration](suite.md) for configuration and security boundaries.
Back up the stopped data directory before upgrading. Shared sign-in creates an
additive identity-binding table when configured. Keep the pre-upgrade backup;
use it when returning to older binaries rather than opening a migrated database
with an older application. Publishing a release does not change running services.

# Tidings 0.4.2

- Standardize shipped service configs and documented environment examples on
  `KEY="value"`, including numbers, booleans, and empty values.
- Identify the service manager and installed config path in every template.
- Check source and generated package configs, literal quoting, and OpenRC
  environment inheritance during `make check`.

Existing configuration values remain supported and local files are preserved.
No database migration is introduced.

# Tidings 0.4.1

- Use the existing town crier for the favicon, touch icon, and header mark.
  A mascot cache revision replaces the former bell in browser tabs.

- Fix full-story downloads rejected by some publishers, including OpenAI's
  Hacktron announcement. Use a compatible TLS group profile that retains
  X25519MLKEM768 hybrid key exchange, TLS 1.2 or newer, certificate verification,
  and the existing public-address-only connection and redirect checks.
- Accept a broader range of publisher content-negotiation responses, fixing
  HTTP 406 responses from SANS diary pages.
- Show publisher HTTP errors, timeouts, size limits, and extraction failures
  distinctly. Record bounded failure details in server logs without exposing
  article query strings or publisher response bodies. Existing story text stays
  available when a fetch fails.
- Add TLS compatibility and certificate-verification regressions, plus an opt-in
  live check for real article extraction. No database migration is needed.

# Tidings 0.4.0

- Preserve article headings, links, lists, quotations, tables, and code blocks.
  Inline images use the existing optional local cache.
- Add Get full story, using a local Readability parser and the existing public-only
  HTTP client. Downloaded copies belong to each reader and travel with marked
  stories in exports and backups.
- Remove known tracking fields from article links while preserving functional
  query values, anchors, signed URLs, feed addresses, and media addresses.
- Add optional per-reader keyboard shortcuts, off by default. Editing fields and
  browser extension key combinations keep their normal behavior.
- Adapt feed checks between five minutes and 24 hours; back off on failures,
  respect publisher cache/retry timing, and allow one refresh per host at a time.
- Apply saved themes before page content appears to prevent flashes on navigation.
- Migrate transactionally to schema 5. Fetch full feed bodies once on upgrade to
  populate article formatting without changing reading state.

# Tidings 0.3.1

- When feed images are enabled, fetch full feed bodies to populate image
  addresses on stories imported by older parser versions. Previously, publisher
  validators could return “not modified” and leave those addresses empty.
- Start the fetch immediately and show its progress using the live indicator.
  Permission checks, setting changes, and validator resets remain atomic.

# Tidings 0.3.0

- Add browser first-owner setup through `tidings setup`, with a private host key,
  confirmed passwords, and setup closing once an owner exists.
- Align `create-owner` and `set-password` across Comfyware: hidden confirmed input
  by default, plus `--password-prompt`, `--password-stdin`, `--password-file`,
  and `--data-dir`. Password changes end earlier signed-in sessions.
- Use published Comfylib v0.1.8 and retain existing account command aliases.
- Fetch newly followed and imported sources immediately; show live waiting,
  fetching, completed, and failed states with the next scheduled check.
- Use roomier story cards and clearer type, and remove repeated reading caveats.
- Add an owner-controlled option for feed-supplied pictures. Previews are served
  through Tidings and share a private, bounded local cache with seven-day expiry.
- Migrate transactionally to schema 4 with images disabled on upgrade. Backups
  preserve image settings and addresses; cached image files are rebuildable.
- Exercise every bundled publisher using the production fetcher in an opt-in
  live-source check, alongside deterministic browser and security tests.

# Tidings 0.2.0

- Fix OpenRC configuration inheritance for plain assignments, including the
  public address, secure cookies, trusted proxies, and backup settings.
- Give invitations a dedicated page using the shared Comfyware layout. Generated
  links use the configured public origin and are shown only when created.
- Add an owner-only **Fetch all feeds now** action. It fetches all subscribed and
  household feeds, bypasses the five-minute cooldown, coalesces repeated requests,
  and preserves individual subscriptions and reading state.
- Let owners enable suggested-source categories. All categories begin disabled,
  including on upgrade; enabling or disabling them changes suggestions only.
  Add news, science, culture, food, sports, and nature alongside the existing
  AI, tech, and infosec catalogs. The 45 optional feeds are never auto-subscribed.
- Upgrade the database to schema 3 transactionally. Pre-upgrade schema-2 backups
  remain supported; restore a pre-upgrade backup to roll back to version 0.1.0.

# Tidings 0.1.0

The first public release of Tidings. This is an early release for small,
self-hosted installations; read the current scope and limitations in the README.

- RSS and Atom sources, tags, handpicked/tag-based feed groups, and optional shared household sources.
- Independent reading state, saved stories, OPML import/export and versioned JSON export/import.
- Verified automatic and manual backups, restoration, service configuration,
  and account recovery using Comfylib v0.1.7.
- Linux amd64/arm64 archives, Debian packages, source and checksums, versioned
  multi-platform container images, and Gentoo overlay packaging.
- Private service accounts, loopback native defaults, HTTPS proxy examples,
  and tested installation and container lifecycles.

Tidings renders supplied feed text and does not scrape full articles. Personal subscriptions stay personal; shared household reading is opt-in. Native port 8085 avoids Songstead’s port 8083.

[Installation](releases.md) · [Containers](containers.md) · [Hosting](deployment.md) · [Backups](automatic-backups.md)
