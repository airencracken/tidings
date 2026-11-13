# Host Tidings

Native services listen on `127.0.0.1:8085` and keep data in `/var/lib/tidings`.
The data directory is private (0700) and only one server can use it at a time.
Never serve it from your reverse proxy.

## Public address and proxy

Set the public HTTPS address before you invite anyone:

```sh
TIDINGS_BASE_URL="https://tidings.example.org"
TIDINGS_TRUSTED_PROXIES="127.0.0.1/32,::1/128"
```

Trust only proxies you control. Example Caddy, nginx and Apache configurations
are under `deploy/`, and `tidings proxy-config caddy --domain YOUR_DOMAIN`
prints one for your domain. Terminate TLS at the proxy and keep the Tidings
listener private. Containers listen on port 8080 inside the container.

## Configuration files

systemd reads `/etc/tidings/tidings.env`; OpenRC reads `/etc/conf.d/tidings`.
Edit the one for your service manager and restart the service. Quote every
value, including numbers and booleans:

```sh
TIDINGS_BASE_URL="https://tidings.example.org"
TIDINGS_BACKUP_KEEP="7"
```

Upgrades keep your local settings. The
[shared configuration convention](https://github.com/airencracken/comfylib/blob/master/docs/configuration.md)
covers quoting and escaping.

## Service account and commands

Debian and Gentoo packages install `/usr/bin/tidings`; the source service
examples use `/usr/local/bin/tidings`. For a source install, create a `tidings`
user and group and the private state directory first.

Run account, backup and restore commands as the account that owns the database.
Installed commands run with `sudo` switch to the service account themselves.
For a portable install, run them as your own unprivileged user with
`TIDINGS_DATA_DIR` set.

```sh
tidings create-owner --username owner
tidings set-password --username member
tidings backup --destination /var/lib/tidings/manual-backups
```

Commands prompt twice for a password with echo off. For automation, use
`--password-file` with a private file the service account can read. Never put a
password on the command line.

## Logs and shutdown

systemd logs to the journal (`journalctl -u tidings`). OpenRC logs to
`/var/log/tidings.log`, and the included logrotate rule keeps eight weekly
logs. Logs and backups can contain private data.

SIGTERM stops new requests, waits for background work and closes the database.
Use `/healthz` to check readiness.

See [backups and recovery](automatic-backups.md) and [installation and upgrades](releases.md).
