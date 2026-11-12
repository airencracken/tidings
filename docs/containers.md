# Run Tidings with Docker Compose

Published images are at [GHCR](https://github.com/airencracken/tidings/pkgs/container/tidings):
`ghcr.io/airencracken/tidings:v0.5.4`. The exact version tag contains **linux/amd64**
and **linux/arm64**. Docker selects your host architecture automatically. There
is no moving `latest` tag; old release tags are not replaced.

Download `docker-compose.yml` from the matching source release. Set the external
origin in a private `.env` file before starting:

```sh
TIDINGS_BASE_URL="https://tidings.example.org"
```

Then provision the owner with terminal password prompting, and start the service:

```sh
docker compose pull
docker compose run --rm tidings create-owner --username owner
docker compose up -d
```

The example binds `127.0.0.1:8085` to container port 8080. Put your HTTPS reverse
proxy in front. The initial owner command does not require a running server.
The image runs as UID/GID **10001**, uses a mode-0700 named `/data` volume, a
read-only root filesystem, bounded temporary storage, no added capabilities,
and `no-new-privileges`. A bind-mounted directory must be owned by 10001:10001
and private before use. Do not change the container to run as root.

The runtime includes CA certificates, timezone data and zstd. The binary in the
published image is the exact binary from the checksummed release archive.
Native lifecycle tests run on both supported architectures before publishing.
A health check tests `/healthz` using the configured port. Configuration and
backups are covered by the [hosting guide](deployment.md) and
[recovery guide](automatic-backups.md).

Stop before changing to restored data. Named volumes survive container removal;
`docker compose down --volumes` deletes them. Back up before upgrading, edit the
exact image version, then run `docker compose pull` and `docker compose up -d`.

For a source development build, use
`docker compose -f docker-compose.yml -f docker-compose.build.yml up --build`.
This override is separate from the default published-image installation.
