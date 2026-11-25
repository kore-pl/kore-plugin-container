# Container management plugin（kore-plugin-container）

## Overview

This plugin manages existing containers through the host’s installed `docker` CLI, providing listing, concise inspect, logs, start/stop/restart and a single statistics sample. It brings no Docker SDK, no bundled runtime and no direct daemon socket (G-05 / G-17): every external action is a fixed-argv `docker` subprocess (no shell). A missing docker CLI, unreachable daemon or non-Linux platform returns `E_UNSUPPORTED`, with no silent fallback and no fabricated data.

## Protocol surface

Protocol `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, capabilities `system:read` + `system:admin`. Twelve business methods: `list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`, all requiring the `container.` domain prefix. `container.exec` / `shell` / `attach` are explicitly not offered (ADR-0018) and return `E_UNSUPPORTED` before docker is touched. Handshake stdout is reserved for protocol frames; logs always go to stderr.

## Core capabilities

All container operations run through fixed-argv `docker` subprocesses, never a shell; protected containers cannot be stopped or removed; `container.update` only adjusts resource limits; `stats` is a one-shot sample with no ticker or polling (G-19). The config contract `container.config.get` / `container.config.set` defines three real options — `docker_bin`, `protected_containers`, `docker_timeout_ms` — all with Chinese label/help/default; precedence is `config.json` > environment > code defaults, and saving takes effect immediately (`restart_required=false`).

## Security boundary

The deny-list and escape parameters (exec / shell / attach) return `E_UNSUPPORTED` before docker is reached; the protected-container list prevents accidental stops of production containers; when docker.sock is inaccessible it returns `E_NEED_ROOT` per ADR-0018 and points to the kernel privilege-escalation channel rather than suggesting the docker group (equivalent to root); non-Linux / missing CLI / unreachable daemon still return `E_UNSUPPORTED`. The plugin runs as an isolated process with a 0600 socket; bypassing permissions or connecting to another socket is a vulnerability.

## Build and verification

Daily verification uses local `make check` (gofmt → go vet → go test -timeout 200s); before release run `make dist` twice and confirm identical sha256. GitHub Actions is manual-only (`workflow_dispatch`), while Gitea Actions triggers on version tags. `depth_level=basic`: core paths are usable, boundary cases and fault injection are deferred to v1.1 — a positioning choice, not a functional downgrade.

## Related links

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
