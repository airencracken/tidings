# Host Tidings

Native services bind to `127.0.0.1:8085` and use `/var/lib/tidings`. A single
installation has one active server; a lock prevents a second server from opening
that instance. The data directory is private (0700) and the database is 0600.
Do not serve the data directory from your reverse proxy.

Configure the public HTTPS origin before inviting members:

```sh
TIDINGS_BASE_URL=https://tidings.example.org
TIDINGS_TRUSTED_PROXIES=127.0.0.1/32,::1/128
```

Only trust proxies that you control. Examples for Caddy, nginx, and Apache are
included under `deploy/`. Keep the upstream listener private and terminate TLS
at your proxy. Native defaults use 8085; containers use port 8080 internally.

OpenRC accepts ordinary `TIDINGS_*=value` assignments in `/etc/conf.d/tidings`;
`export` is optional. The init script passes the supported settings to the service.

## Services and account commands

The Debian and Gentoo packages use `/usr/bin/tidings`. Source service examples
use `/usr/local/bin/tidings`. Use a dedicated `tidings` user and group, and create
its private state directory before starting a portable service installation.
Systemd and OpenRC pass configuration through the environment. Their examples
use a 0077 umask, run without root privileges, and preserve private state.

Run owner creation, password recovery, backup and restore commands as the same
account that owns the database. Installed administrative commands invoked as root
resolve the service settings and re-run as its configured account. Unmanaged
root administration is refused. Portable installations should use an explicitly
configured unprivileged account and `TIDINGS_DATA_DIR`.

```sh
tidings create-owner --username owner
tidings set-password --username member
tidings backup --destination /var/lib/tidings/manual-backups
```

Never put a password in command-line arguments. Interactive entry disables
terminal echo and asks for confirmation. Automation can use --password-file with a private file readable by the service account.

## Logs and shutdown

Systemd writes to the journal (`journalctl -u tidings`). OpenRC writes to
`/var/log/tidings.log`; the included logrotate rule retains eight compressed
weekly logs. Logs and backups may contain private data; protect them.

A normal SIGTERM closes the listener, waits for background work, and closes the
database. Check `/healthz` for readiness. Do not use process existence as the
only readiness signal. Start the service and use browser setup, or create the first owner through the CLI.

See [backup and recovery](automatic-backups.md) and [installation/upgrades](releases.md).
