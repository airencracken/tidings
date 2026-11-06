# Backups and recovery

The running service makes a verified, consistent SQLite snapshot automatically
every day, beginning about a minute after startup. The database includes all
instance data. Automatic archives are retained under the private data directory.
Defaults: seven copies, 10 GiB total retained bytes, and a 5 GiB uncompressed
snapshot limit. zstd is preferred when installed; built-in gzip is the fallback.
Restoring a zstd archive requires the `zstd` program.

The `TIDINGS_BACKUP_INTERVAL`, `TIDINGS_BACKUP_KEEP`,
`TIDINGS_BACKUP_MAX_BYTES`, and `TIDINGS_BACKUP_COPY_MAX_BYTES` settings control
these limits. Setting the interval to `0` disables automatic backups. Owners
can inspect backup success or failure from the settings/account interface.

A manual verified backup can run while the server is active:

```sh
tidings backup --destination /var/lib/tidings/manual-backups
```

This prints the complete verified backup directory. Pass that directory, including manifest.json and its snapshot archive, to restore.

Stop the service before switching to a restored instance. Restore refuses an
existing destination and validates the snapshot before exposing restored data:

```sh
tidings restore --archive /private/tidings-backup --destination /private/tidings-restored
```

Run the command as the service account, point your configuration at the restored
private directory, and start the service. Confirm sign-in and your content before
removing the old copy. Keep protected snapshots on another machine and practice
restoring them. Backups contain credentials, sessions and private contributions.
A member's personal data export is not a complete instance backup.

Do not copy only a live SQLite database file while its WAL journal is active.
A stopped-service copy of the complete private data directory is an alternative.
