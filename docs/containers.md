# Run Tidings with Docker Compose

Images are published to [GHCR](https://github.com/airencracken/tidings/pkgs/container/tidings)
as `ghcr.io/airencracken/tidings:v0.8.0`, for linux/amd64 and linux/arm64.
There is no `latest` tag, and release tags never change.

Download `docker-compose.yml` from the matching source release. Put your public
address in a private `.env` file next to it:

```sh
TIDINGS_BASE_URL="https://tidings.example.org"
```

Create the owner and start the service:

```sh
docker compose pull
docker compose run --rm tidings create-owner --username owner
docker compose up -d
```

The example publishes `127.0.0.1:8085` to container port 8080; put your HTTPS
reverse proxy in front. The container runs as UID and GID 10001 with a read-only
root filesystem and a private named `/data` volume. If you bind-mount a
directory instead, make it private and owned by 10001:10001. Do not run the
container as root.

To upgrade, back up, change the image version in `docker-compose.yml`, then run
`docker compose pull` and `docker compose up -d`. Named volumes survive
`docker compose down`; `docker compose down --volumes` deletes them.

See the [hosting guide](deployment.md) for settings and
[backups and recovery](automatic-backups.md) for restores.

To build from source instead, run
`docker compose -f docker-compose.yml -f docker-compose.build.yml up --build`.
