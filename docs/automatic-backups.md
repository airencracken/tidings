# Backups and recovery

The running server makes a verified SQLite snapshot every day, starting about a
minute after it starts. Automatic backups live in the private `backups`
directory inside the data directory. By default Tidings keeps seven, up to
10 GiB in total, and refuses a snapshot larger than 5 GiB uncompressed. Archives
use zstd when it is installed and gzip otherwise; restoring a zstd archive needs
the `zstd` program.

Change these limits with `TIDINGS_BACKUP_INTERVAL`, `TIDINGS_BACKUP_KEEP`,
`TIDINGS_BACKUP_MAX_BYTES` and `TIDINGS_BACKUP_COPY_MAX_BYTES`. An interval of
`0` turns automatic backups off. Owners see the last success or failure under
**Admin → Instance backups**.

## Manual backup

You can make a manual backup while the server runs:

```sh
tidings backup --destination /var/lib/tidings/manual-backups
```

The command prints the backup directory. Tidings never deletes manual backups.

## Restore

Restore into a new, empty private directory:

```sh
tidings restore --archive /private/tidings-backup --destination /private/tidings-restored
```

Restore checks the archive and the database before writing anything, and
refuses a directory that already holds an instance. Run it as the service
account. Check the restored copy on a spare loopback port, then stop the
service, point `TIDINGS_DATA_DIR` at the restored directory and start it again.
With the supplied systemd unit, also update `WorkingDirectory` and
`ReadWritePaths`. Keep the old directory until you have checked the new one.

## Keep them safe

Backups contain passwords, sessions and every member's reading data. Keep them
private, copy them to another machine, and practise restoring. A member's JSON
export is not a backup of the instance.

Do not copy the live database file on its own while the server runs. A copy of
the whole data directory taken with the service stopped also works.
