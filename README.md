# NORA v2

Self-hosted chat for small groups. No federation, no tracking, no cloud dependency. Runs on a cheap VPS (512MB RAM).

## What is this

A Discord-like chat server + native desktop client. Built for 5-20 people who want their own private communication platform.

- **Server**: Go API with SQLite, zero external services
- **Client**: Native Go + [Gio](https://gioui.org) UI, GPU-rendered, no browser engine
- **Auth**: ed25519 challenge-response, no passwords on the server
- **Encryption**: E2E encrypted DMs (ECDH + AES-256-GCM) and groups (AES-256-GCM)

## Features

**Chat**: Text channels, DMs, groups, threads, reactions, polls, pins, search, message scheduling, auto-moderation

**Voice**: WebRTC with Opus codec (48kHz), noise gate, per-user volume, speaking indicators

**Media**: Chunked resumable uploads, inline image preview, video player (ffmpeg), P2P file transfer, swarm sharing

**Organization**: Hierarchical categories, roles with permissions, channel permission overrides, kanban boards, calendar with recurring events, collaborative whiteboard

**Networking**: WireGuard VPN tunnels, LAN party channels, game server management (Docker, RCON, room access with iptables firewall)

**Security**: Device fingerprinting, invite chain tracking, quarantine mode, approval mode, IP/device bans, rate limiting

## Build

```bash
# Server
make server

# Native client (requires CGO for audio)
make client

# Windows cross-compile (requires MinGW)
make client-windows

# Tests
make test          # server
make test-client   # client
```

## Deploy

The server is a single Go binary + SQLite database. Configure via `nora.toml` (see `nora.example.toml`).

```bash
./nora
```

Default port: 9021.

## License

AGPL-3.0
