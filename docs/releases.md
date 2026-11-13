# Install Tidings

[GitHub releases](https://github.com/airencracken/tidings/releases) have Linux
amd64 and arm64 archives, Debian packages, a source archive and SHA-256
checksums. The current release is **0.8.0**. To create the first owner, see
[setup](setup.md).

## Linux archive

Download the archive for your architecture and the checksum file, then run
`sha256sum -c tidings_0.8.0_checksums.txt --ignore-missing` in that directory.
The archive holds the program, licences, guides, and service and proxy examples
under `deploy/`.

Run Tidings as an unprivileged account with `TIDINGS_DATA_DIR` set to a private
directory. `tidings --help` lists commands and `tidings --version` shows the
installed version.

## Debian or Ubuntu

```sh
sudo apt install ./tidings_0.8.0_amd64.deb
sudo tidings create-owner --username owner
sudo systemctl enable --now tidings
```

Use the arm64 package on arm64. The package creates a `tidings` service account,
a private `/var/lib/tidings` and `/etc/tidings/tidings.env`. Installing does not
start the service; upgrading restarts it if it was running. Removing or purging
the package keeps your configuration and data.

## Gentoo

Add the [Comfyware overlay](https://github.com/airencracken/comfyware#add-the-overlay)
and install `www-apps/tidings`. Configure `/etc/conf.d/tidings` for OpenRC or
`/etc/tidings/tidings.env` for systemd, then run
`tidings create-owner --username owner` as root. With OpenRC, start the service
with `rc-service tidings start` and add it to a runlevel with
`rc-update add tidings default`.

## Containers and source

See the [container guide](containers.md). Source builds need Go 1.26.11 or
newer and use only the modules pinned in `go.mod`.

## Upgrades

Back up, stop the service, install the new version and start it again. Startup
migrates the database. An older version cannot open a migrated database; to roll
back, restore your pre-upgrade backup into a new directory.

## Rebuild a release tag

Maintainers can run the Release workflow on `master` with `release_tag` set to an
existing tag. It checks the tag, rebuilds and retests it, and can finish an
unpublished draft. Published releases and image tags are never replaced.
