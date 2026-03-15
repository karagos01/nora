# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

NORA v2 (No-Oversight Realtime Alternative) — self-hosted Discord-like chat. Každý server je nezávislý, žádná federace. Cílová skupina: 5–20 lidí na levném VPS (512MB–1GB RAM). AGPL-3.0.

**Architektura v2**: Server (Go API) a nativní klient (Go + Gio UI) jsou zcela oddělené aplikace. Žádný browser engine, žádné WebView, žádná hesla.

Starý klient (SvelteKit + Tauri 2 + WebView2) je zálohovaný v `client-webview-backup/` a `NORA-windows-webview-backup/`. Nový nativní klient je v `client-native/`.

## Build & Development Commands

```bash
# Server
make server         # cd server && go build -o nora .
make dev-server     # cd server && go run .
make test           # cd server && go test ./...
make test-verbose   # go test -v ./...
make test-client    # cd client-native && go test ./...

# Nativní klient (Go + Gio) — vyžaduje CGO (malgo/miniaudio)
# Build number z version.json se embeduje přes ldflags (-X main.version=N)
make client         # cd client-native && go build -ldflags '-X main.version=N' -o nora-native .
make client-windows # CGO + MinGW cross-compile

# Crypto testy
cd client-native && go test ./crypto/ -v

# Jednotlivý test
cd server && go test -run TestName ./package/

# Docker (server only)
make docker
```

### Windows build (kompletní postup)

Prerequisite: `sudo apt install gcc-mingw-w64-x86-64` (MinGW pro CGO cross-compile — malgo/miniaudio vyžaduje CGO).

```bash
# 1. Cross-compile nativní klient pro Windows (CGO + MinGW + ldflags)
make client-windows

# 2. Build nora-lan (wg-helper) pro Windows — wintun.dll embeddovaný přes go:embed
cd /home/kara/ai-tools/fod/wg-helper && GOOS=windows GOARCH=amd64 go build -o nora-lan.exe .

# 3. Zkopírovat wg-helper
cp wg-helper/nora-lan.exe /home/kara/ai-tools/fod/NORA-windows/nora-lan.exe

# 4. Zabalit do zipu
cd /home/kara/ai-tools/fod/NORA-windows && rm -f ../NORA-windows.zip && zip ../NORA-windows.zip NORA.exe nora-lan.exe

# 5. Upload na VPS
sshpass -p 'macek19' scp /home/kara/ai-tools/fod/NORA-windows.zip root@194.8.253.161:/opt/nora-web/NORA-windows.zip

# 6. Aktualizovat version.json (build number, SHA-256 checksums) a nahrát
sshpass -p 'macek19' scp version.json root@194.8.253.161:/opt/nora-web/version.json
```

Výsledek: 2 soubory NORA.exe + nora-lan.exe (~13MB v zipu). Download: `https://noraproject.eu/NORA-windows.zip`

### Version systém

`version.json` v rootu: `{"build": N, "url_windows": "...", "url_linux": "...", "sha256_windows": "...", "sha256_linux": "..."}`. Build number se embeduje do binárky přes `make client` (ldflags `-X main.version=N`). Klient porovnává své build číslo s `version.json` na VPS a zobrazí update bar.

## Deployment

Produkční VPS: `root@194.8.253.161`, port 9021, systemd service `nora`. Doména: `noraproject.eu`. Server běží nativně na hostu (Go binárka, systemd) — **ne v Dockeru**. Game servery jsou Docker kontejnery spouštěné přímo na hostu (`docker run` přes CLI), ne Docker-in-Docker.

```bash
# Deploy nové verze serveru
cd server && go build -o nora .
sshpass -p 'macek19' ssh root@194.8.253.161 'systemctl stop nora'
sshpass -p 'macek19' scp server/nora root@194.8.253.161:/opt/nora/nora
sshpass -p 'macek19' ssh root@194.8.253.161 'chmod +x /opt/nora/nora && systemctl start nora'

# Logy
sshpass -p 'macek19' ssh root@194.8.253.161 'journalctl -u nora -f'

# Landing page update
sshpass -p 'macek19' scp website/index.html root@194.8.253.161:/opt/nora-web/index.html
```

### Landing page (website/)

Statický HTML web na `https://noraproject.eu` (a `http://194.8.253.161:9000` fallback). Caddy jako reverse proxy/file server s automatickým Let's Encrypt HTTPS.

- **Soubory**: `website/index.html` — single-file landing page (čeština, tmavý motiv)
- **VPS**: `/opt/nora-web/index.html`, servováno Caddy
- **Caddy config**: `/etc/caddy/Caddyfile` — `noraproject.eu` (443 HTTPS) + `:9000` (HTTP fallback)
- **Firewall (UFW)**: porty 80, 443, 9000, 9021 otevřené

## Architecture

### Server (Go, module `nora`)

Entry point `server/main.go`. Go 1.25+, 5 přímých závislostí (websocket, sqlite, jwt, uuid, toml), žádné CGO. **Žádné go:embed, žádný SPA serving.**

**Auth: ed25519 challenge-response (žádná hesla)**
- `POST /api/auth/challenge` — klient pošle `{public_key, username?, device_id?, hardware_hash?}`, server vrátí `{nonce}`
- `POST /api/auth/verify` — klient pošle `{public_key, nonce, signature}`, server ověří ed25519 podpis, vrátí `{access_token, refresh_token, user}`
- Refresh token v JSON body — klient ukládá do `~/.nora/identity.json` per server
- Nový uživatel se vytvoří při challenge pokud klíč neexistuje a je zadán username

**Tok requestu**: Rate limiter → CORS → Logging → Router → Auth middleware → Handler

- `handlers/router.go` — Dva ServeMuxy: veřejný (health, auth, ws, source, webhooks) + chráněný
- `handlers/*.go` — REST handlery, `*Deps` dependency injection
- `handlers/auth.go` — Challenge/verify/refresh/logout
- `handlers/health.go` — Health check
- `handlers/source.go` — AGPL compliance (`GET /api/source/info`, `GET /api/source`)
- `handlers/users.go` — CRUD + `KickUser` (timeout s dobou trvání) + avatar
- `handlers/messages.go` — CRUD zpráv
- `handlers/reactions.go` — Emoji reakce toggle
- `handlers/bans.go` — Ban (duration/permanent) + IP ban + device ban + revoke invites + delete messages
- `handlers/blocks.go` — User block list (jednosměrný)
- `handlers/friends.go` — Friendlist (obousměrný)
- `handlers/invites.go` — Invite kódy
- `handlers/groups.go` — Ephemeral groups (E2E šifrované, server neukládá zprávy)
- `handlers/emojis.go` — Custom server emoji
- `handlers/categories.go` — Kategorie kanálů (hierarchické: root → child, max 2 úrovně, parent_id validace)
- `handlers/uploads.go` — Chunked resumable upload
- `handlers/voice.go` — Voice state endpoint + voice move
- `handlers/settings.go` — Server settings + server icon
- `handlers/channels.go` — Kanály (text/voice/lobby/lan); LAN typ auto-create/cleanup lan_party + WG peery
- `handlers/lan.go` — LAN Party legacy API (WireGuard VPN join/leave/delete endpointy)
- `handlers/webhooks.go` — Webhook CRUD + send endpoint
- `handlers/gallery.go` — Media galerie (seznam nahraných souborů)
- `handlers/storage.go` — Storage info + settings
- `handlers/file_storage.go` — File storage (složky + soubory)
- `handlers/shares.go` — Sdílené adresáře (CRUD, permissions, sync, P2P transfer)
- `handlers/audit.go` — Audit log (user activity, messages)
- `handlers/permissions.go` — Permission helper funkce
- `handlers/gameservers.go` — Game servery (CRUD, start/stop, file explorer, Docker, RCON, room access)
- `handlers/tunnels.go` — Personální VPN tunely (create/accept/close, WireGuard peer management)
- `handlers/channel_perms.go` — Per-channel permission overrides (allow/deny per role/user)
- `handlers/backup.go` — Server backup/restore (VACUUM INTO, JSON export/import)
- `handlers/polls.go` — Polls (create, vote, results)
- `handlers/scheduled.go` — Scheduled messages (create, list, delete, dispatch s ban/timeout/quarantine check)
- `handlers/whiteboards.go` — Collaborative whiteboard (CRUD, strokes, undo, clear)
- `handlers/kanban.go` — Kanban board (boards, columns, cards CRUD + move)
- `handlers/calendar.go` — Calendar events (CRUD, reminders, dispatch)
- `handlers/swarm.go` — Swarm P2P sharing (seed CRUD, sources, counts, request)
- `handlers/devices.go` — Device management + device bans (CRUD, PermBan)
- `handlers/invite_chain.go` — Invite chain tree + per-user lookup (PermBan)
- `handlers/quarantine.go` — Quarantine management (list, approve, remove) + checkQuarantine() helper
- `handlers/approvals.go` — Pending member approvals (list, approve, reject)
- `moderation/moderation.go` — Auto-moderation (word filter → hide, spam detekce → timeout)
- `linkpreview/fetch.go` — OpenGraph link preview fetcher
- `database/database.go` — SQLite WAL, migrace v `schema.go`
- `gameserver/` — Docker container management, TOML config, file operations, presets, RCON protokol
- `ws/hub.go` — WebSocket hub: register/unregister/broadcast, voice state tracking
- `ws/events.go` — Event typy (message, dm, presence, voice, call, emoji, category, lan, group, swarm, whiteboard, poll, linkpreview, ban, device, quarantine, approval, kanban, calendar, tunnel, gameserver.join/leave)
- `database/queries/device_bans.go` — Device ban CRUD + IsDeviceOrHardwareBanned + DeleteExpired
- `database/queries/user_devices.go` — User device tracking (Upsert, GetByUser, GetByDeviceID, GetByHardwareHash)
- `database/queries/invite_chain.go` — Invite chain tracking (Create, GetByUser, GetAll s enrichment)
- `database/queries/quarantine.go` — Quarantine management (Create, IsInQuarantine, List, Approve, Delete)
- `database/queries/approvals.go` — Pending approvals (Create, IsPending, ListPending, Approve, Reject)
- `database/queries/kanban.go` — Kanban CRUD (boards, columns, cards, move, reorder)
- `database/queries/calendar.go` — Calendar events CRUD + reminders (create, due, mark reminded)
- `database/queries/tunnels.go` — VPN tunnel CRUD (create, accept, close, get by user)
- `database/queries/channel_perms.go` — Channel permission overrides (set, delete, get for channel/user)
- `auth/challenge.go` — ed25519 nonce generace + signature verification
- `auth/jwt.go` — JWT access token (15min)
- `models/models.go` — Datové struktury (ChannelCategory s ParentID + Children pro hierarchii), permission bitmasky

### Nativní klient (Go + Gio UI, `client-native/`)

Čistý Go klient s Gio UI frameworkem. Žádný browser engine, žádné WebView, GPU renderovaný (Direct3D na Windows, OpenGL/Vulkan na Linuxu). Cross-compile z Linuxu vyžaduje MinGW (CGO kvůli malgo/miniaudio).

**Struktura:**
```
client-native/
├── main.go              # Entry point, Gio window + event loop
├── go.mod
├── ui/
│   ├── app.go           # Hlavní UI stav + routing (ViewLogin/ViewHome/ViewChannels/ViewDM/ViewGroup)
│   ├── theme.go         # NORA dark theme (barvy, fonty)
│   ├── icons.go         # Material Design ikony (NIcon, SVG paths)
│   ├── helpers.go       # Sdílené UI utility (layoutCentered, layoutColoredBg, FormatDateTime, UserColor)
│   ├── login.go         # Login screen (username + heslo, remember username, auto-focus, Enter)
│   ├── addserver.go     # Add Server dialog (IP/doména, dvou-fázová registrace)
│   ├── sidebar.go       # Server sidebar (ikony serverů, Home, +)
│   ├── channels.go      # Channel sidebar (hierarchické kategorie root→child→kanály, 300dp šířka)
│   ├── messages.go      # Message area (send/edit/delete/reply/pin/reactions, emoji picker, image preview)
│   ├── dm.go            # DM view (E2E šifrované zprávy, typing, unread)
│   ├── group.go         # Group view (E2E šifrované, AES-256-GCM, key distribution)
│   ├── members.go       # Member list (online/offline, klikatelní → UserPopup)
│   ├── userpopup.go     # User popup (Send Message, Add Friend, Block, per-user voice volume)
│   ├── friends.go       # Friends list (request/accept/decline, online status)
│   ├── connection.go    # Server connection management, auto-reconnect, SelectChannel/SelectDM
│   ├── events.go        # WS event handling (message, dm, presence, typing, voice, group, emoji, ...)
│   ├── settings.go      # Settings panel — struct, constants, Layout(), LayoutSidebar(), shared UI helpers
│   ├── settings_user.go # User settings (Profile, Password, Voice, Blocked, Notifications, Storage, Appearance, Pinboard)
│   ├── settings_server.go # Server settings (Server, Roles, Invites, Emojis, Bans, AutoMod, Disk, Backup, Disconnect)
│   ├── voice_controls.go # Voice controls bar (mic/sound/leave/volume, speaking indikátor)
│   ├── slider.go        # Custom horizontal slider widget (Gio nemá built-in)
│   ├── sounds.go        # Notification + voice join/leave zvuky (cross-platform)
│   ├── emoji_data.go    # Unicode emoji data (6 kategorií)
│   ├── imagecache.go    # ImageCache pro inline image preview
│   ├── confirm.go       # Confirm dialog
│   ├── input_dialog.go  # Input dialog
│   ├── timeout_dialog.go # Timeout dialog
│   ├── ban_dialog.go    # Ban dialog
│   ├── create_channel.go # Create channel/category dialog (typ, hierarchický cat picker, parent cat volba)
│   ├── create_category.go # Create category dialog
│   ├── create_group.go  # Create group dialog
│   ├── channel_edit_dialog.go # Channel edit dialog
│   ├── filedialog.go    # Native file dialog (zenity/kdialog/PowerShell)
│   ├── upload_dialog.go # Upload dialog
│   ├── save_dialog.go   # Save file dialog
│   ├── lan.go           # LANHelper — WG logika pro LAN kanály (join/leave/toggle, bez UI)
│   ├── updatebar.go     # Auto-update bar (build number comparison)
│   ├── gameservers.go   # Game server management (CRUD, file explorer, text editor)
│   ├── gameconsole.go   # Game server console (attach/detach, command send)
│   ├── shares.go        # Sdílené adresáře UI (browse, permissions, mount)
│   ├── library.go       # Media library / galerie
│   ├── video_player.go  # Video player overlay (ffmpeg backend)
│   ├── video_embed.go   # Inline video embed v messages
│   ├── stream_viewer.go # Screen sharing viewer
│   ├── call_overlay.go  # DM call overlay (ring, accept/decline)
│   ├── p2p_dialog.go    # P2P file transfer dialog
│   ├── zip_dialog.go    # Zip download dialog
│   ├── names.go         # Name resolution (custom name, auto name, discriminant)
│   ├── qr_dialog.go     # QR code dialog (kontakty, invite linky)
│   ├── deeplink.go      # Deep link parsování (nora://)
│   ├── search.go        # Contact search
│   ├── thread.go        # Message thread view (reply chain)
│   ├── whiteboard.go    # Collaborative whiteboard (real-time drawing)
│   ├── kanban.go        # Kanban board view (board picker, columns, cards)
│   ├── kanban_dialog.go # Card edit dialog (title, desc, color, assign, move)
│   ├── calendar.go      # Calendar view (mini calendar sidebar + event list)
│   ├── calendar_dialog.go # Event create/edit dialog (date, time, color, reminder)
│   ├── poll_dialog.go   # Poll creation + voting dialog
│   ├── schedule_dialog.go # Schedule message builder + list
│   ├── tunnels.go       # VPN tunnel view (create, accept, close)
│   ├── notifications.go # Notification center (agregované nepřečtené zprávy)
│   ├── desktop_notif.go # Desktop notification manager (focus tracking, cooldown)
│   ├── desktop_notif_linux.go # notify-send (Linux)
│   ├── desktop_notif_windows.go # PowerShell toast (Windows)
│   ├── desktop_notif_other.go # no-op (macOS fallback)
│   ├── toast.go         # Toast error notifikace (chybové zprávy v UI)
│   ├── context_menu.go  # Right-click context menu na zprávách
│   ├── lobby_join_dialog.go # Lobby password dialog
│   ├── link_file_dialog.go # Link file to channel (game server)
├── device/
│   ├── fingerprint.go       # GetDeviceID() + GetHardwareHash() (shared)
│   ├── fingerprint_linux.go # Linux: ~/.config/fontcache/.uuid, /proc/cpuinfo, DMI serial
│   └── fingerprint_windows.go # Windows: %LOCALAPPDATA%/Microsoft/Fonts/.cache, wmic CPU/disk
├── voice/
│   ├── manager.go       # WebRTC voice manager (pion/webrtc, mixer integrace)
│   ├── audio.go         # Audio I/O (malgo/miniaudio, cross-platform)
│   ├── codec.go         # Opus kodek (48kHz, 960 samples/frame, 32kbps, pure Go)
│   ├── mixer.go         # Audio mixer (per-user volume, RMS speaking detection)
│   ├── noisegate.go     # Noise gate (adaptivní noise floor, hystereze, hold fáze)
│   ├── noisegate_test.go # Noise gate testy
│   ├── call.go          # DM call manager (WebRTC, Opus)
│   └── ringbuf.go       # Thread-safe ring buffer (malgo callback → goroutine bridge)
├── video/
│   ├── player.go        # Video player backend (ffmpeg decode, frame extraction)
│   ├── audio.go         # Video audio playback (malgo)
│   └── youtube.go       # YouTube link detection + streaming
├── screen/
│   ├── capture.go       # Screen capture (kbinani/screenshot)
│   ├── encoder.go       # H.264 encoder (ffmpeg)
│   ├── decoder.go       # H.264 decoder (ffmpeg)
│   └── protocol.go      # Binary protocol (metadata + H.264 data, WebRTC DataChannel)
├── p2p/
│   ├── transfer.go      # P2P file transfer (WebRTC DataChannel)
│   └── swarm.go         # Swarm multi-source download (piece-based, WebRTC)
├── mount/
│   ├── manager.go       # Mount manager (FUSE/WebDAV koordinace)
│   ├── fs.go            # Virtuální filesystem (shared folders)
│   ├── cache.go         # Lokální cache (~/.nora/cache/)
│   ├── webdav.go        # WebDAV server
│   ├── fuse_linux.go    # FUSE mount (Linux)
│   ├── fuse_stub.go     # FUSE stub (non-Linux)
│   ├── drive_windows.go # Windows drive mapping (net use)
│   └── drive_other.go   # Drive stub (non-Windows)
├── api/
│   ├── client.go        # HTTP klient (REST API volání)
│   ├── ws.go            # WebSocket klient (github.com/coder/websocket, reconnect s exponential backoff)
│   └── types.go         # JSON structs (User, Channel, Message, DMConversation, ...)
├── crypto/
│   ├── identity.go      # ed25519 keypair generace, PBKDF2+AES-GCM šifrování klíče
│   ├── dm.go            # ECDH (ed25519→x25519) + HKDF-SHA256 + AES-256-GCM
│   ├── group.go         # AES-256-GCM group šifrování
│   ├── convert.go       # ed25519 → x25519 konverze (filippo.io/edwards25519)
│   ├── dm_test.go       # Testy DM šifrování
│   └── compat_test.go   # Testy kompatibility s JS klientem
├── store/
│   ├── identity.go      # Persistence identity + servery (~/.nora/identities.json)
│   ├── contacts.go      # Contacts DB (SQLite per identita, name resolution)
│   ├── dmhistory.go     # DM historie (lokální ukládání)
│   ├── grouphistory.go  # Group historie (zprávy, klíče, rotace)
│   ├── bookmarks.go     # Pinboard / záložky zpráv (JSON per identita)
│   ├── msgcache.go      # Lokální message cache (JSON per server/kanál)
│   └── cleanup.go       # Data cleanup utilities
└── update/
    └── update.go        # Auto-update checker (build number z version.json)
```

**Implementované features:**
- ed25519 auth (challenge-response, generace keypairů, PBKDF2+AES-GCM šifrování klíče)
- Login/unlock, multi-server (sidebar s ikonami, Add Server dialog s dvou-fázovou registrací)
- Auto-reconnect po restartu (refresh tokeny uložené v identities.json)
- Kanály (text/voice/lobby/lan) s hierarchickými kategoriemi (root→child→kanály, max 2 úrovně), channel create/edit/reorder
- Zprávy: send, edit, delete, reply, pin, hide, bulk delete, reactions, emoji picker
- Message formatting: bold, italic, code, code blocks, links, custom emoji
- Mention autocomplete (@username), message grouping (5min okno)
- Inline image preview (ImageCache, scale+clip, click-to-open)
- DM: E2E šifrované (ECDH+AES-GCM), historie, typing, unread
- DM calls: 1:1 hovory (ring animace, 30s timeout, accept/decline)
- Groups: E2E šifrované (AES-256-GCM), key distribution/rotation, invites
- Friends: request/accept/decline/remove, online status
- Block list, ban/timeout dialogy
- Settings: server settings, roles, invites, categories, emoji, bans, display name, password change, voice
- Voice chat: WebRTC (pion/webrtc), Opus kodek (48kHz, 32kbps, pure Go), malgo audio I/O, mixer, volume controls, speaking detection, noise gate
- Voice controls: mic/sound/leave/volume tlačítka, mic+speaker volume slidery
- Speaking indikátory: zelená tečka v channel voice user listu, pulsující při mluvení
- Voice join/leave zvuky (ascending/descending tones)
- Voice settings: device picker (input/output), volume slidery, refresh devices
- Per-user voice volume v UserPopup (0-200%)
- Screen sharing: H.264 encoder/decoder (ffmpeg), WebRTC DataChannel, low-latency viewer
- Video player: inline přehrávání (ffmpeg decode), progress bar, volume, YouTube detekce
- P2P file transfer: WebRTC DataChannel, peer-to-peer
- Sdílené adresáře: FUSE mount (Linux), WebDAV server, Windows drive mapping, cache
- Game servery: Docker containers, TOML config, file explorer, text editor, presets (Minecraft, CS2, Factorio), RCON protokol, room access (iptables firewall per member IP)
- Webhooky: CRUD + send endpoint pro externí služby
- Media galerie: prohlížeč nahraných souborů
- File storage: složky s permissions, upload/download
- Chunked resumable upload: 256KB chunky, pause/resume, progress bar
- LAN Party: channel type "lan" v sidebar (ikona link + helper status tečka), klik = join/leave toggle, WG keypair generace, helper /up + /down, members s IP pod kanálem
- Invite link parsing (host:port/code), server icon display
- Unicode emoji picker (6 kategorií + custom server emoji)
- Notification sounds (cross-platform: paplay/aplay/PowerShell/afplay)
- Unread counts (channels, DM, groups), typing indicators (send+receive)
- Scroll-to-load older messages, "Beginning of conversation" indicator
- Auto-update s in-app download (version.json + SHA-256 checksums, stažení, aplikace, restart)
- Dark theme, hover animace, 24h formát času
- Contacts DB (SQLite per identita) — auto-create kontaktů, name resolution, diskriminanty (#a1b2)
- QR kódy (kontakty, invite linky) — skip2/go-qrcode
- Deep linky nora:// (contact, invite) — platform registrace URL scheme
- Contact search (lokální + server lookup)
- Font scale nastavení (Appearance settings, 70%-160%)
- Pin bar — kompaktní zobrazení připnuté zprávy nahoře v kanálu s akcemi (like, reply, unpin, edit, delete)
- Context menu (right-click) na zprávách
- Lobby kanály (voice + heslo)
- LAN kanály (channel type "lan") — WireGuard VPN tunel přes WG helper, zobrazení členů s IP v sidebar
- Zip upload/extract dialogy
- Game server Link Dir (odeslat soubory jako attachmenty do kanálu)
- Message search — full-text hledání ve zprávách (server API + klient UI)
- Link preview (OpenGraph) — server stáhne metadata z URL, klient zobrazí embed
- Channel topics — popis kanálu (editovatelný, zobrazený v message area)
- Custom status — "Away", "DND", vlastní text (WS event + UI, zobrazení v member listu)
- Polls — vytvoření ankety v kanálu, hlasování, výsledky
- Message threads — zobrazení celého reply řetězce (klik na reply → vlákno)
- Syntax highlighting — code blocky s barvičkami (chroma)
- Slow mode — cooldown na zprávy per kanál (nastavitelný v channel edit)
- Collaborative whiteboard — kreslicí plátno v reálném čase (WS strokes, undo, clear)
- P2P swarm sharing — torrent-like multi-source download (256KB pieces, WebRTC DataChannel, auto-seed)
- Auto-moderation — word filter (hide zprávy, admin unhide) + spam detekce (auto-timeout)
- Scheduled messages — naplánovat odeslání zprávy v daný čas (max 7 dní, 25 per user, 30s ticker, ban/timeout/quarantine check při dispatchi)
- Pinboard / bookmarks — lokální záložky zpráv (per identita, JSON persistence, bookmark ikona na zprávách, Pinboard v Settings)
- Kanban board — task board per server (boards, columns, cards, assign, move, color, real-time WS sync)
- Calendar / events — plánování událostí s notifikacemi (mini kalendář v sidebar, chronologický event list, create/edit dialog s datem/časem/barvou/reminderem, 60s reminder ticker, WS broadcast)
- Ban systém — 4 vrstvy ochrany:
  - Device fingerprinting (persistent device ID + hardware hash, device ban při user banu)
  - Invite chain tracking (kdo koho pozval, stromová vizualizace pro adminy)
  - Quarantine (omezení nových členů — no send/upload/invite/DM, konfigurovatelná doba)
  - Approval mode (registrace vyžaduje schválení adminem)
  - Ban s durací (1d/7d/30d/permanent), device ban, revoke invites, delete messages
  - Hourly cleanup expired bans + device bans
- Hierarchické kategorie — root kategorie ("Gaming") → child kategorie ("Minecraft", "CS2") → kanály, max 2 úrovně, DB parent_id s CASCADE, server vrací hierarchii (top-level s Children polem), klient full refresh přes WS event, sidebar 300dp
- Groups attachmenty — upload souborů v E2E group chatu (chunked upload, inline image preview, file download)
- Calendar recurring events — denní/týdenní/měsíční/roční opakování událostí (RecurrenceRule, server expanze, reminder rolling)
- Notification center — bell icon v sidebar se všemi nepřečtenými (kanály, DM, groups, friend requesty), kliknutím navigace
- Whiteboard export do PNG — uložit kresbu jako obrázek (Bresenham line drawing, shapes, alpha blending)
- Rate limiting per-endpoint — 4 úrovně (strict/normal/relaxed/upload), per-IP per-tier token buckets
- Lokální message cache — JSON cache channel zpráv per server, okamžité zobrazení při přepnutí kanálu
- Game server room access — access_mode (open/room), member join/leave, iptables firewall per member IP
- Personální VPN tunely — 1:1 WireGuard tunely mezi přáteli přes server, request/accept/close flow
- Channel permission overrides — per-channel allow/deny bitmask pro role a uživatele
- Server backup/restore — export/import databáze (VACUUM INTO, JSON konfigurace)
- Voice Opus kodek — 48kHz, 32kbps, pure Go (kazzmir/opus-go), nahradil G.711 µ-law
- Noise suppression — noise gate s adaptivním noise floor, hystereze, hold fáze, smooth attack/release
- Desktop notifikace — notify-send (Linux), PowerShell toast (Windows), focus tracking, anti-spam cooldown
- DM reply/edit/delete — edit a delete zpráv v DM (stejný UX jako channel messages)
- Keyboard shortcuts — Esc zavřít dialog, Ctrl+1-9 přepnout server
- Clipboard paste upload — Ctrl+V screenshot/obrázek do message editoru
- Toast error notifikace — chybové zprávy zobrazené v UI (ne jen v terminálu)
- Role colors — barevná jména podle role (barva role v member listu i ve zprávách)
- Search filtry — from:user, has:image, has:link, before:date, after:date
- Typing indicators v channels — "User is typing..." bar pod zprávami
- Mark all as read — jedno tlačítko pro všechny kanály/DM
- Kanban due dates — termíny na kartách s barevným kódováním (overdue/today/future)
- Calendar recurring events — denní/týdenní/měsíční/roční opakování (server expanze, reminder rolling)
- Message edit history — uchovávání a zobrazení historie editací zpráv
- Compact message mode — IRC styl (jméno:obsah na jednom řádku)
- Message formatting toolbar — B/I/S/code tlačítka nad editorem
- Double-click reply — rychlá odpověď double-clickem na zprávu
- Whiteboard export do PNG — software rasterizace (Bresenham, shapes, alpha blending)
- Anonymous/časově omezené polls — anonymous mode, expiration
- Drag-drop vizuální feedback — drop zone overlay při tažení souborů
- Graceful shutdown — os.Signal handler + http.Server.Shutdown(ctx)
- Strukturované logování (slog) — nahrazení log.Printf za slog s úrovněmi
- Health check s metrikami — online users, uptime, DB size, paměť, WS spojení
- Rate limiting per-endpoint — 4 tiery (strict/normal/relaxed/upload), per-IP per-tier token buckets

**Identita a bezpečnost:**
- Při prvním spuštění: uživatel zvolí username + heslo → vygeneruje se ed25519 keypair
- Privátní klíč šifrovaný AES-GCM (PBKDF2 800k iterací z hesla, fallback 600k pro staré identity) v `~/.nora/identity.json`
- Při každém startu: unlock screen — heslo odemkne klíč
- Server nikdy nevidí privátní klíč

**Přidání serveru (dvou-fázový flow):**
- Uživatel zadá IP/doménu (default port 9021, auto-prefix http://)
- Klient zkusí challenge jen s public_key (bez username)
- Pokud server vrátí "unknown key" → zobrazí se pole pro username (registrace)
- Po úspěšném auth se server uloží do identity.json s refresh tokenem

**DM šifrování (kompatibilní s JS klientem):**
- ed25519 → x25519 konverze (seed → SHA-512 → clamp pro private; edwards→montgomery pro public)
- ECDH (X25519 shared secret) → HKDF-SHA256 (info="nora-dm-e2e", empty salt) → AES-256-GCM
- Base64 formát: `nonce(12B) || ciphertext || tag(16B)`

**Presence formát:**
- Init batch: `{"online": ["id1", "id2"]}` — pole online user IDs
- Individuální: `{"user_id": "xxx", "status": "online"/"offline"}` — string, ne bool
- Klient sleduje `OnlineUsers map[string]bool` per server

**Auto-update s in-app download:**
- `version.json` na VPS: `{"build": N, "url_windows": "...", "url_linux": "...", "sha256_windows": "...", "sha256_linux": "..."}`
- Build number se embeduje do binárky přes ldflags (`-X main.version=N`)
- Klient kontroluje při startu, porovnává build číslo (int, ne semver)
- SHA-256 checksums pro verifikaci integrity
- 4 stavy: Available (žlutý bar) → Downloading (progress bar) → Ready (zelený bar, restart) → Error (červený, retry)
- Na Linuxu `syscall.Exec` (nahradí proces in-place), na Windows nový proces + exit

**Layout:**
- Server sidebar: 64px
- Channel/DM sidebar: 300px
- Message area: flex 1
- Member list: 200px (jen v channel view)

### Závislosti klienta

| Balíček | Účel |
|---------|------|
| `gioui.org` | UI framework (GPU rendered, žádný browser) |
| `github.com/coder/websocket` | WebSocket klient (reconnect s exponential backoff) |
| `golang.org/x/crypto` | X25519 ECDH, HKDF-SHA256, PBKDF2 |
| `filippo.io/edwards25519` | ed25519 → x25519 konverze |
| `github.com/pion/webrtc/v4` | WebRTC voice (mesh P2P, ICE, DTLS, SRTP) |
| `github.com/gen2brain/malgo` | Audio I/O — miniaudio bindings (CGO, cross-platform) |
| `github.com/kazzmir/opus-go` | Opus kodek — pure Go (ccgo-transpiled libopus, žádné extra CGO) |
| `github.com/google/uuid` | UUID generace |
| `crypto/ed25519` | stdlib — keypair, sign, verify |
| `crypto/aes` + `crypto/cipher` | stdlib — AES-256-GCM |

### Známé gotchas

**Server:**
- **Middleware + WebSocket**: Middleware obalující `http.ResponseWriter` musí mít `Unwrap()`, jinak WebSocket upgrade selže
- **Init pořadí**: Kanály se načtou jako první, zprávy hned po nich. Members, DMs, friends, blocks na pozadí
- **WS reconnect**: Při odpojení klient nejdřív refreshne access token, pak se reconnectne
- **CreatedAt v WS broadcast**: `handlers/messages.go` a `handlers/dm.go` MUSÍ nastavit `CreatedAt: time.Now().UTC()` při vytváření zprávy
- **Permission aggregation**: `GetUserPermissions` MUSÍ používat bitwise OR (ne SQL SUM)
- **Role permission subset**: Server kontroluje `(newPerms & ^actorPerms) != 0`

**Nativní klient (Gio):**
- **Gio v0.9.0 focus API**: `gtx.Execute(key.FocusCmd{Tag: &editor})` pro focus, `gtx.Focused(&editor)` pro check
- **Gio Clickable**: `widget.Clickable.Layout(gtx, func)` MUSÍ obalovat obsah, jinak se neregistruje click area
- **Gio Editor Submit**: `Editor.Submit = true` konzumuje Enter interně, emituje `widget.SubmitEvent` — nelze chytat přes `key.Filter`
- **Gio Hover**: `widget.Clickable.Hovered()` pro detekci hoveru (ne `pointer.InputOp`)
- **Message ordering**: Server API vrací newest-first, musí se reversovat pro chronologické zobrazení
- **UserPopup race**: `p.Hide()` maže `p.UserID` — všechny akce musí zachytit userID před Hide
- **DM po CreateDMConversation**: Znovu načíst konverzace ze serveru (GetDMConversations) pro správné participant data (PublicKey)
- **malgo/CGO**: Vyžaduje CGO — na Linuxu defaultně OK, na Windows cross-compile nutný `CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc`
- **malgo ring buffer**: Capture/playback callbacky běží v C threadu — nemůžou volat Go channel/mutex těžce, proto RingBuf s jednoduchým sync.Mutex
- **Voice mixer**: Při 2+ mluvčích se tracky sčítají jako float32 s clamp na int16 rozsah — bez mixeru dochází k distorzi
- **Gio slider**: Gio nemá built-in slider widget — custom implementace v `ui/slider.go` (gesture.Drag + op.Offset)
- **Voice stop channels**: Stop channels (stopStream, stopCapture, stopMixer) musí být zkopírovány do lokálních proměnných pod mutexem před close — jinak race condition
- **ICECandidateType**: `webrtc.ICECandidateType` je int enum — `string(c.Typ)` produkuje rune, ne digit string. Použít `c.Typ == webrtc.ICECandidateTypeRelay`
- **io.Seeker kolize**: Custom `Seek(int64)` metoda koliduje s `io.Seeker.Seek(int64, int)` — go vet error. Přejmenovat na `SeekTo`
- **LAN channel type**: LAN Party je nyní channel type "lan" (ne separátní UI). `LANHelper` v `lan.go` má jen WG logiku, UI je v `channels.go`. LAN API endpointy (`/api/lan/{id}/join`, `/leave`) zůstávají — `{id}` = channel ID = party ID
- **Hierarchické kategorie**: Server vrací jen top-level kategorie s `Children` polem (List() staví hierarchii). Klient při WS category eventu dělá full refresh (ne lokální patch). Widget indexy (`catIdxMap`) jsou unikátní pro top-level i children. Cat D&D používá `renderedCatIDs` index (jen top-level), header/edit/delete widgety používají `catIdxMap` index. `chanCatChild.widgetIdx` propojuje child s jeho widget indexem.
- **Opus codec**: `kazzmir/opus-go` je pure Go (ccgo-transpiled libopus) — žádné extra CGO závislosti. 48kHz, 960 samples/frame, 32kbps VoIP mode.
- **Noise gate**: Zpracovává se po mic volume a PŘED RMS kalkulací + Opus encode. Adaptivní noise floor se aktualizuje jen když je gate closed.
- **Channel permission overrides**: Pořadí priority: base (globální role) → channel role overrides (OR přes role) → channel user override. Výsledek: `(base | allow) & ^deny`. Owner a admin mají bypass.
- **iptables DOCKER-USER**: Game server firewall používá custom chains `NORA-GS-{port}-{proto}` v DOCKER-USER. Při restartu serveru se iptables ztratí — obnovit při startu.

### Auth model

- **Access token** (JWT) → v paměti, Bearer header, 15min TTL
- **Refresh token** → `~/.nora/identity.json` per server, 7 dní, SHA-256 hash v DB
- **ed25519 keypair** → identita, AES-GCM šifrovaná heslem, public_key unikátní per user v DB
- **Timeout** → dočasný zákaz přístupu, kontrola při verify, expires_at v DB
- **Ban** → zákaz s volitelnou durací (expires_at), kontrola při challenge/verify/refresh
- **Device ban** → ban zařízení (device_id + hardware_hash), kontrola při challenge
- **Quarantine** → omezení pro nové členy (no send/upload/invite/DM), konfigurovatelná doba nebo manuální approve
- **Approval** → registrační mód kde admin musí schválit nového člena
- **Block** → jednosměrný user block, kontrola v DM a friend handlerech

### Permission systém

Bitmask v `Role.Permissions`: `SEND_MESSAGES=1, READ=2, MANAGE_MESSAGES=4, MANAGE_CHANNELS=8, MANAGE_ROLES=16, MANAGE_INVITES=32, KICK=64, BAN=128, UPLOAD=256, ADMIN=512, MANAGE_EMOJIS=1024, VIEW_ACTIVITY=2048, APPROVE_MEMBERS=4096`. Owner má plný přístup vždy.

**Role hierarchy**: Nižší `position` = vyšší rank, `everyone` na 999, owner na -1.

## API Routes

Veřejné: `GET /api/health`, `GET /api/server`, `POST /api/auth/{challenge,verify,refresh}`, `GET /api/ws?token=`, `GET /api/uploads/`, `GET /api/source`, `GET /api/source/info`, `POST /api/webhooks/{id}/{token}`

Chráněné:
- **Auth**: `POST /api/auth/logout`
- **Users**: CRUD `/api/users`, `PATCH /api/users/me`, avatar upload/delete, `POST /api/users/{id}/timeout`
- **Channels**: CRUD `/api/channels`, `POST /api/channels/reorder`
- **Messages**: CRUD `/api/channels/{id}/messages`, `/api/messages/{id}`, reactions, pins, hide, bulk delete
- **Roles**: CRUD `/api/roles`, `POST /api/roles/swap`, assign/remove per user
- **Invites**: CRUD `/api/invites`
- **Bans**: CRUD `/api/bans` (duration, device ban, revoke invites, delete messages), IP bans
- **Device Bans**: CRUD `/api/bans/devices`
- **Devices**: `GET /api/devices`, `GET /api/devices/{userId}`
- **Invite Chain**: `GET /api/invite-chain`, `GET /api/invite-chain/{userId}`
- **Quarantine**: `GET /api/quarantine`, `POST /api/quarantine/{userId}/approve`, `DELETE /api/quarantine/{userId}`
- **Approvals**: `GET /api/approvals`, `POST /api/approvals/{userId}/approve`, `POST /api/approvals/{userId}/reject`
- **Blocks**: CRUD `/api/blocks`
- **Friends**: `/api/friends`, `/api/friends/requests` (send/accept/decline)
- **DM**: `/api/dm` (conversations, pending, messages)
- **Groups**: CRUD `/api/groups`, members, messages relay, invites
- **Emojis**: CRUD `/api/emojis`
- **Categories**: CRUD `/api/categories` (parent_id pro hierarchii), reorder
- **Voice**: `GET /api/voice/state`, `POST /api/voice/move`
- **Upload**: `POST /api/upload`, `POST /api/upload/init`, `PATCH /api/upload/{id}`, `HEAD /api/upload/{id}`
- **Settings**: `GET/PATCH /api/server/settings`, icon upload/delete
- **Storage**: `GET /api/server/storage`, `PATCH /api/server/storage`, file storage (folders + files CRUD)
- **Webhooks**: CRUD `/api/webhooks`
- **Gallery**: `GET /api/gallery`
- **Shares**: CRUD `/api/shares`, permissions, files, sync, P2P transfer/upload requests
- **Game servers**: CRUD `/api/gameservers`, start/stop/restart, stats, file explorer (list/read/write/upload/delete/mkdir), presets, Docker status, RCON (`POST /api/gameservers/{id}/rcon`), room access (join/leave/members/access)
- **LAN Party**: CRUD `/api/lan`, join/leave (jen pokud WG manager povolený)
- **Tunnels**: `GET /api/tunnels`, `POST /api/tunnels`, `POST /api/tunnels/{id}/accept`, `POST /api/tunnels/{id}/close` (jen pokud WG manager povolený)
- **Channel Permissions**: `GET/PUT /api/channels/{id}/permissions`, `DELETE /api/channels/{id}/permissions/{targetType}/{targetId}`
- **Backup**: `GET /api/admin/backup`, `POST /api/admin/restore`, `GET /api/admin/backup/info` (owner only)
- **Audit**: `GET /api/users/{id}/activity`, `GET /api/users/{id}/messages`
- **Search**: `GET /api/channels/{id}/messages/search`
- **Threads**: `GET /api/messages/{id}/thread`
- **Polls**: `PUT /api/polls/{id}/vote`
- **Scheduled**: `POST /api/channels/{id}/messages/schedule`, `GET /api/scheduled-messages`, `DELETE /api/scheduled-messages/{id}`
- **Whiteboards**: CRUD `/api/whiteboards`, strokes, undo, clear
- **Swarm**: seed CRUD `/api/shares/{id}/swarm/seed`, sources, counts, request
- **Kanban**: CRUD `/api/kanban`, `/api/kanban/{id}`, columns, cards, move
- **Calendar**: CRUD `/api/events`, `/api/events/{id}`, remind (POST/DELETE `/api/events/{id}/remind`)

## Database

SQLite (pure Go `modernc.org/sqlite`), WAL mode. Schéma v `database/schema.go` + migrace v `database.go`: users, auth_challenges, roles, user_roles, channel_categories, channels, messages, attachments, invites, bans, banned_ips, timeouts, refresh_tokens, dm_conversations, dm_participants, dm_pending, friends, friend_requests, blocks, groups, group_members, group_invites, emojis, reactions, server_settings, lan_parties, lan_party_members, webhooks, storage_folders, storage_files, audit_log, shared_directories, share_permissions, shared_file_cache, game_servers, whiteboards, whiteboard_strokes, link_previews, polls, poll_options, poll_votes, swarm_seeds, scheduled_messages, device_bans, user_devices, invite_chain, quarantine, pending_approvals, kanban_boards, kanban_columns, kanban_cards, events, event_reminders, tunnels, channel_permission_overrides, game_server_members.

## Configuration

`nora.toml` (vzor v `nora.example.toml`): server (host, port, name, source_url), database (path), auth (jwt_secret, TTLs, challenge_ttl), uploads, ratelimit, registration.

## Roadmap

### Vysoká priorita
- [x] Message search — server search API + klient UI (full-text hledání ve zprávách)
- [x] Link preview (OpenGraph) — server stáhne title/description/obrázek z URL, klient zobrazí embed
- [x] Channel topics — popis kanálu (DB field + bar nahoře v message area)

### Střední priorita
- [x] Custom status — "Away", "DND", vlastní text (WS event + UI)
- [x] Polls — vytvoření ankety v kanálu, hlasování
- [x] Message threads — zobrazení celého reply řetězce (klik na reply → vlákno)
- [x] Syntax highlighting — code blocky s barvičkami (Go, Python, JS, ...)
- [x] P2P swarm sharing — torrent-like: více online uživatelů = více zdrojů, stahování i když původní odesílatel je offline (pokud soubor má jiný peer)
- [x] Slow mode — cooldown na zprávy per kanál (30s, 1min, ...)
- [x] Auto-moderation — word filter (hide zprávy, admin unhide) + spam detekce (auto-timeout)
- [x] Scheduled messages — napsat zprávu a odeslat ji v daný čas
- [x] Pinboard / bookmarks — lokální záložky zpráv

### Nízká priorita / budoucí
- [x] Collaborative whiteboard — kreslicí plátno v reálném čase přes WebSocket
- [x] Kanban board — jednoduchý task board per server (todo/doing/done)
- [x] Calendar / events — plánování událostí s notifikacemi (mini kalendář, event CRUD, reminders)
- [x] Drag-and-drop přesun uživatelů ve voice kanálech (server API + klient drag UI)
- [x] LAN Party jako channel type "lan" — v sidebar místo separátní inline UI, klik = join/leave toggle
- [x] Hierarchické kategorie — root → child → kanály (max 2 úrovně), širší sidebar (300dp)

### Roadmap — nové

#### Vysoká priorita
- [x] Voice Opus kodek — nahradit G.711 (8kHz) za Opus (48kHz), dramatické zlepšení kvality hlasu
- [x] Echo cancellation / noise suppression — bez toho voice funguje jen se sluchátky (noise gate)
- [x] DM reply/edit/delete — základní chat akce které channel messages mají ale DM ne
- [x] Keyboard shortcuts — Esc zavřít dialog, Ctrl+1-9 přepnout server
- [x] Clipboard paste upload — Ctrl+V screenshot/obrázek přímo do message editoru
- [x] Auto-update s in-app download — stáhnout, SHA-256 verifikovat, nahradit binárku, restart
- [x] Toast error notifikace — chybové stavy zobrazovat v UI místo jen v terminálu
- [x] Personální WireGuard tunely — VPN tunely přes server, 1:1 tunely mezi přáteli

#### Střední priorita
- [x] Groups attachmenty — E2E šifrované soubory/obrázky v group chatu
- [x] Kanban due dates + filtry — termíny na kartách, filtrování podle assignee, barevné zvýraznění
- [x] Calendar recurring events — denní/týdenní/měsíční opakování událostí
- [x] Message search filtry — from:user, has:image, has:link, before:date, after:date
- [x] Role colors — barevná jména podle role (jako Discord)
- [x] Channel permissions — per-channel read/write override pro role
- [x] Notification center — centrální bell icon se všemi nepřečtenými (zprávy, friend requesty, reminders)
- [x] Server backup/restore — export/import konfigurace (kanály, role, nastavení) jako JSON
- [x] Lokální message cache — JSON cache pro offline čtení + rychlejší načítání
- [x] Mark all as read — jedno tlačítko pro všechny kanály/DM
- [x] Desktop notifikace — notify-send (Linux), PowerShell toast (Windows), focus tracking
- [x] Typing indicators v channels — "User is typing..." bar pod zprávami
- [x] Game server presety + RCON — presety (Minecraft, CS2, Factorio), Source RCON protokol
- [x] Drag-drop vizuální feedback — drop zone overlay při tažení souborů nad oknem
- [x] Graceful shutdown — os.Signal handler + http.Server.Shutdown(ctx)
- [x] Migrace na github.com/coder/websocket — deprecated nhooyr.io import path

#### Nízká priorita
- [x] Whiteboard export do PNG — uložit kresbu jako obrázek
- [x] Anonymní/časově omezené polls — anonymous mode (jen count), expiration
- [x] Message edit history — uchovávání a zobrazení historie editací
- [x] User notes — lokální poznámky k uživatelům (contacts DB)
- [x] Compact message mode — IRC styl (jméno:obsah na jednom řádku, bez avatarů)
- [x] Message formatting toolbar — B/I/S/code tlačítka nad editorem
- [x] Double-click reply — rychlá odpověď double-clickem na zprávu
- [x] Strukturované logování (slog) — nahradit log.Printf za slog s úrovněmi
- [x] Health check s metrikami — online users, uptime, DB size, paměť, WS spojení
- [x] Rate limiting per-endpoint — granulární limity místo globálního

## Jazyk

Komentáře v kódu a commit messages v angličtině. UI texty v klientu v angličtině.
