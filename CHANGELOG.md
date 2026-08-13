# Changelog

All notable changes to this project are documented in this file. This project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-13

Initial public release.

### Added

- Local and remote tmux session management from a shared `rses.yaml`.
- Top-level `groups` blocks that organize sessions and assign their group
  without repeated per-session fields.
- `rses init [DIR]` with an embedded starter configuration and official
  providers, plus `-C DIR` for explicit configuration-directory selection.
- Selection by session name, group, tag, remote host, and dependency graph.
- Background control daemon with persisted runtime records and streamed
  lifecycle progress.
- Interactive watch view with per-host batched snapshots, status, logs, and
  in-flight indicators.
- Inspectable generated scripts through `rses explain`.
- `rses version` and `rses --version` output shared with daemon build metadata.
- Embedded `ssh-tunnel` and `cloudflared-tcp` providers, plus an external
  provider protocol for project-specific integrations.
- Separate provider stdout JSON and stderr diagnostics channels.
- Configurable operation, query, and SSH connection timeouts with
  non-interactive remote queries by default.
- Linux and macOS release archives for amd64 and arm64.
- Go-based integration coverage for the real CLI, daemon, tmux lifecycle,
  embedded initialization, and SSH tunnel reconnect behavior.
- Third-party dependency notices included in every release archive.

[Unreleased]: https://github.com/bnkrr/rses/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/bnkrr/rses/releases/tag/v0.1.0
