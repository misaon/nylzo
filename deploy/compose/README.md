# Self-host Nylzo with Docker Compose

The English deployment guide is in the [GitHub Wiki](https://github.com/misaon/nylzo/wiki/Self-hosting). Its [reviewed source](../../docs/wiki/Self-hosting.md) stays in this repository.

Start with [release verification](../../docs/wiki/Verify-a-release.md), then follow the guide to configure a digest-pinned image, TLS, the server pepper, the reporting key, and production limits. Use Linux with Docker Engine for production so source-address rate limits work as intended.

This directory contains the deployment files:

- [compose.yaml](compose.yaml): containers, private storage, secrets, proxy trust, networking, and bounded logs.
- [Caddyfile](Caddyfile): TLS and request handling.
- [.env.example](.env.example): configuration template; replace the image placeholder with a verified digest.

The [operations reference](../../docs/operations.md) covers configuration, upgrades, pepper rotation, storage requirements, and abuse reports.
