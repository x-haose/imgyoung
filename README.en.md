# ImgYoung

[简体中文](./README.md) · [Docs](https://imgyoung.x-haose.com/en/docs) · [Releases](https://github.com/x-haose/imgyoung/releases)

Self-hosted image hosting with multi-channel failover. Every image is stored in several storage channels (object
storage, cloud drives, local disk — any rclone backend); when one goes down, uploads and reads switch to the next.
Channels can be encrypted down to ciphertext, and the metadata database snapshots itself into them. One binary, one
Docker command.

## Features

- **Multi-channel failover**: priority defines primary and backups, and health checks demote failing channels. A failed upload moves on to the next channel, you can require N synchronous copies, and replication jobs survive restarts.
- **Channel encryption**: built on rclone crypt, so the provider sees only ciphertext. Keys derive from the master key, and stock rclone can decrypt the data on its own.
- **Automatic metadata backups**: scheduled snapshots go to the highest-priority healthy channel; browse them in the admin console and restore with one click.
- **Lifecycle**: expiry at three levels with renew-on-access; images nobody opens for a long time land in a cleanup queue for an admin to judge.
- **Deduplication**: byte-identical images merge automatically; perceptually similar ones are shown side by side for an admin to decide.
- **Many ways to upload**: HTTP API, PicGo / PicList, the S3 protocol, URL import, and drag-and-drop in the admin console, all through one pipeline.
- **On-demand processing**: resize with `?w=` to preset widths and serve WebP to browsers that accept it; original URLs never change, so a CDN can cache them for a long time.
- **Admin console**: multiple users, named tokens, quotas and rate limits, audit log, alerts, metadata export; English and Simplified Chinese, dark and light themes.

## Quick start

```bash
docker run -d --name imgyoung --restart unless-stopped \
  -p 8721:8721 -v imgyoung-data:/data ghcr.io/x-haose/imgyoung:latest
```

On Linux or macOS you can also install it as a local program in one line (`sh -s -- --service` also registers a systemd service):

```bash
curl -fsSL https://raw.githubusercontent.com/x-haose/imgyoung/main/install.sh | sh
```

Open `http://localhost:8721/`, sign in as `admin` / `imgyoung123` (you will be asked to change the password), issue a token on the "Upload tokens" page, and upload:

```bash
curl -H "Authorization: Bearer <token>" -F "file=@a.png" http://localhost:8721/api/upload
# {"id":"R0RjUyP1UW","url":"http://localhost:8721/i/R0RjUyP1UW"}
```

A new instance has only the built-in local channel, so images and backups share one disk. Add an off-site channel on the "Storage channels" page before you rely on failover.

## Documentation

The full documentation lives at <https://imgyoung.x-haose.com>:

| Section | Covers |
| --- | --- |
| [Install](https://imgyoung.x-haose.com/en/docs/install) | One-line script, Docker, Docker Compose, release archives, building from source |
| [Storage channels](https://imgyoung.x-haose.com/en/docs/channels) | Failover and replication, object storage and WebDAV, cloud drive authorization, channel encryption |
| [Upload clients](https://imgyoung.x-haose.com/en/docs/guides/clients) | PicGo / PicList, the S3 protocol, S3 clients in general |
| [Configuration](https://imgyoung.x-haose.com/en/docs/configuration) | Environment variables and runtime settings |
| [Operations](https://imgyoung.x-haose.com/en/docs/operations/reverse-proxy) | Reverse proxy, CDN, backup and restore, upgrades, password reset, monitoring |
| [Go library](https://imgyoung.x-haose.com/en/docs/library) | Storage drivers and failover primitives |
| [API reference](https://imgyoung.x-haose.com/en/docs/api) | Generated from [api/openapi.yaml](./api/openapi.yaml) |

Example configurations (Compose, Caddy, Nginx, systemd) are in [deploy/](./deploy/); design and technology choices are on the [Architecture](https://imgyoung.x-haose.com/en/docs/architecture) page; planned work is tracked in issues labeled [`roadmap`](https://github.com/x-haose/imgyoung/issues?q=label%3Aroadmap).

## Using it as a Go library

The storage engine is a public package you can `go get` on its own: `store/driver` defines the driver interface, `store/rclone` implements it on any rclone backend (with an optional encryption layer), and `store/failover` provides failover primitives over a set of drivers — write until N copies succeed, read from the first that can, health checks.

```bash
go get github.com/x-haose/imgyoung/store/failover github.com/x-haose/imgyoung/store/rclone
```

See the [Go library docs](https://imgyoung.x-haose.com/en/docs/library) for usage and examples.

## Development

```bash
task test   # all tests (-race)
task e2e    # end-to-end tests: build the real binary, start it, and validate every response against the OpenAPI document
task lint   # Go, admin console, docs site and install script checks; zero issues required
task docs   # preview the docs site locally
```

Environment, conventions, commits and the release process are described in [CONTRIBUTING.en.md](./CONTRIBUTING.en.md).

## License

[MIT](./LICENSE). The admin console reuses generic components from [shadcn-admin](https://github.com/satnaing/shadcn-admin); its license is at [web/licenses/shadcn-admin.LICENSE](./web/licenses/shadcn-admin.LICENSE).
