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
