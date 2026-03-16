# CLAUDE.md

## Project Overview

NORA (No-Oversight Realtime Alternative) — self-hosted Discord-like chat. Každý server je nezávislý, žádná federace. Cílová skupina: 5–20 lidí na levném VPS (512MB–1GB RAM). AGPL-3.0.

Server (Go API) a nativní klient (Go + Gio UI) jsou oddělené aplikace. Žádný browser engine, žádné WebView, žádná hesla (ed25519 challenge-response auth). Klient v `client-native/`.

## Build & Development

```bash
# Server
make server         # go build -o nora
make dev-server     # go run
make test           # server testy
make test-client    # klient testy

# Nativní klient (vyžaduje CGO — malgo/miniaudio)
make client         # Linux (ldflags -X main.version=N z version.json)
make client-windows # CGO + MinGW cross-compile

# Jednotlivý test
cd server && go test -run TestName ./package/
```

### Windows build

Prerequisite: `sudo apt install gcc-mingw-w64-x86-64`

```bash
make client-windows
cd wg-helper && GOOS=windows GOARCH=amd64 go build -o nora-lan.exe .
cp wg-helper/nora-lan.exe NORA-windows/nora-lan.exe
cd NORA-windows && rm -f ../NORA-windows.zip && zip ../NORA-windows.zip NORA.exe nora-lan.exe
sshpass -p 'macek19' scp NORA-windows.zip root@194.8.253.161:/opt/nora-web/NORA-windows.zip
sshpass -p 'macek19' scp version.json root@194.8.253.161:/opt/nora-web/version.json
gh release create vN NORA-windows.zip client-native/nora-native --title "Build N" --notes "changelog"
```

### Version systém

`version.json` v rootu: `{"build": N, "url_windows", "url_linux", "sha256_windows", "sha256_linux"}`. Build number embedovaný přes ldflags. Klient porovnává build číslo s VPS, zobrazí update bar, stáhne + SHA-256 verify + restart.

## Deployment

VPS: `root@194.8.253.161`, port 9021, systemd `nora`. Doména: `noraproject.eu`. Server běží nativně (ne Docker). Game servery = Docker kontejnery na hostu.

```bash
# Deploy serveru
cd server && go build -o nora .
sshpass -p 'macek19' ssh root@194.8.253.161 'systemctl stop nora'
sshpass -p 'macek19' scp server/nora root@194.8.253.161:/opt/nora/nora
sshpass -p 'macek19' ssh root@194.8.253.161 'chmod +x /opt/nora/nora && systemctl start nora'

# Logy
sshpass -p 'macek19' ssh root@194.8.253.161 'journalctl -u nora -f'

# Landing page
sshpass -p 'macek19' scp website/index.html root@194.8.253.161:/opt/nora-web/index.html
```

Landing page: `website/index.html`, Caddy reverse proxy (HTTPS + `:9000` HTTP fallback), UFW: 80, 443, 9000, 9021.

## Architecture

### Server (Go, module `nora`)

Entry point `server/main.go`. Go 1.25+, žádné CGO. Request flow: Rate limiter → CORS → Logging → Router → Auth middleware → Handler.

- `handlers/router.go` — dva ServeMuxy: veřejný + chráněný
- `handlers/*.go` — REST handlery, `*Deps` dependency injection
- `database/database.go` — SQLite WAL, schéma v `schema.go`
- `database/queries/*.go` — DB queries per doména
- `ws/hub.go` — WebSocket hub (register/unregister/broadcast, voice state)
- `ws/events.go` — event typy (message, dm, presence, voice, call, category, group, ...)
- `auth/` — ed25519 challenge + JWT (15min)
- `models/models.go` — datové struktury, permission bitmasky
- `gameserver/` — Docker management, TOML config, presets, RCON
- `moderation/` — word filter + spam detekce
- `linkpreview/` — OpenGraph fetcher
- `wg/` — WireGuard manager (LAN, tunnels)

**Auth flow**: challenge (public_key → nonce) → verify (nonce + ed25519 signature → JWT + refresh token). Nový user se vytvoří při challenge pokud klíč neexistuje a je zadán username.

### Nativní klient (`client-native/`)

Go + Gio UI, GPU rendered. Cross-compile vyžaduje MinGW (CGO kvůli malgo).

**Adresáře:**
- `ui/` — Gio UI (app.go = routing, messages.go = zprávy, channels.go = sidebar, dm.go, group.go, settings*.go, voice_controls.go, ...)
- `voice/` — WebRTC voice (pion/webrtc, Opus 48kHz, malgo I/O, mixer, noise gate)
- `video/` — Video player (ffmpeg, yt-dlp YouTube)
- `screen/` — Screen sharing (H.264, WebRTC DataChannel)
- `p2p/` — P2P file transfer + swarm (WebRTC DataChannel)
- `mount/` — Sdílené adresáře (FUSE Linux, WebDAV, Windows drive mapping)
- `api/` — REST client + WebSocket (coder/websocket)
- `crypto/` — ed25519 identity, ECDH DM šifrování, AES-256-GCM groups
- `store/` — persistence (~/.nora/identities.json, DM/group historie, msgcache, bookmarks)
- `device/` — device fingerprinting (device_id + hardware_hash)
- `update/` — auto-update checker
- `gio-xdnd/` — patched Gio fork (X11 XDND drag-drop)

**Layout**: Server sidebar 64px | Channel sidebar 300px | Message area flex 1 | Member list 200px

**Identita**: username + heslo → ed25519 keypair, privátní klíč AES-GCM šifrovaný (PBKDF2 800k iterací, fallback 600k). Server nikdy nevidí privátní klíč.

**DM šifrování**: ed25519→x25519 konverze → ECDH → HKDF-SHA256 (info="nora-dm-e2e") → AES-256-GCM. Base64: `nonce(12B) || ciphertext || tag(16B)`.

**Presence**: Init batch `{"online": ["id1",...]}`, individuální `{"user_id", "status": "online"/"offline"}`.

## Známé gotchas

**Server:**
- Middleware obalující ResponseWriter musí mít `Unwrap()` (WebSocket upgrade)
- `CreatedAt: time.Now().UTC()` MUSÍ být nastaveno v message/DM handlerech při broadcast
- `GetUserPermissions` MUSÍ používat bitwise OR (ne SQL SUM)
- Role permission subset: `(newPerms & ^actorPerms) != 0`

**Klient (Gio):**
- Focus: `gtx.Execute(key.FocusCmd{Tag: &editor})`, check: `gtx.Focused(&editor)`
- Clickable MUSÍ obalovat obsah (`widget.Clickable.Layout(gtx, func)`)
- `Editor.Submit = true` konzumuje Enter, emituje `widget.SubmitEvent`
- Hover: `widget.Clickable.Hovered()`
- XDND: patched Gio fork, `app.FileDropChan`, XdndAware na WM frame window
- Message ordering: server vrací newest-first, reversovat
- UserPopup: `p.Hide()` maže `p.UserID` — zachytit userID před Hide
- DM po CreateDMConversation: znovu načíst konverzace (pro PublicKey)
- malgo ring buffer: C thread callbacky → RingBuf s sync.Mutex
- Voice stop channels: zkopírovat do lokální proměnné pod mutexem před close
- `ICECandidateType` je int enum — `string()` produkuje rune, použít `== webrtc.ICECandidateTypeRelay`
- Custom `Seek()` koliduje s `io.Seeker` — pojmenovat `SeekTo()`
- Hierarchické kategorie: server vrací top-level s `Children` polem, klient full refresh přes WS event
- Root channel slots: `rootChanSlot map[string]int`, in-memory only
- Opus: `kazzmir/opus-go` pure Go, 48kHz, 960 samples/frame, 32kbps
- Noise gate: po mic volume, PŘED RMS + Opus encode
- Channel perms: `(base | allow) & ^deny`, owner/admin bypass
- iptables DOCKER-USER: custom chains `NORA-GS-{port}-{proto}`, ztráta při restartu
- YouTube: yt-dlp `-j` pro info+URL, kkdai/youtube fallback (403 SABR)
- Video sync: `videoReady` atomic, audio čeká na první video frame
- ffmpeg: `-user_agent`/`-reconnect` jen pro HTTP URL, ne lokální soubory

## Auth & Permission model

- **Access token** (JWT): v paměti, Bearer header, 15min
- **Refresh token**: ~/.nora/identity.json per server, 7 dní, SHA-256 hash v DB
- **Ban**: volitelná durace, device ban (device_id + hardware_hash), kontrola při challenge/verify/refresh
- **Quarantine**: omezení nových členů, konfigurovatelná doba nebo manuální approve
- **Approval**: registrace vyžaduje schválení adminem

**Permission bitmask** (`Role.Permissions`): SEND_MESSAGES=1, READ=2, MANAGE_MESSAGES=4, MANAGE_CHANNELS=8, MANAGE_ROLES=16, MANAGE_INVITES=32, KICK=64, BAN=128, UPLOAD=256, ADMIN=512, MANAGE_EMOJIS=1024, VIEW_ACTIVITY=2048, APPROVE_MEMBERS=4096. Owner má vždy plný přístup. Nižší `position` = vyšší rank.

## API Routes

Veřejné: `GET /api/health`, `GET /api/server`, `POST /api/auth/{challenge,verify,refresh}`, `GET /api/ws?token=`, `GET /api/uploads/`, `GET /api/source{,/info}`, `POST /api/webhooks/{id}/{token}`

Chráněné: users, channels, messages (+ reactions/pins/hide/bulk-delete/search/threads), roles, invites, bans (+ devices + IP), blocks, friends, DM, groups, emojis, categories, voice, upload (chunked), settings, storage, file-storage, webhooks, gallery, shares (+ swarm), gameservers (+ RCON + room-access), LAN, tunnels, channel-permissions, backup/restore, audit, polls, scheduled-messages, whiteboards, kanban, calendar/events, devices, invite-chain, quarantine, approvals.

## Database

SQLite WAL (`modernc.org/sqlite`), schéma v `database/schema.go`, migrace v `database.go`.

## Configuration

`nora.toml` (vzor: `nora.example.toml`): server (host, port, name, source_url), database (path), auth (jwt_secret, TTLs), uploads, ratelimit, registration.

## Jazyk

Komentáře v kódu a commit messages v angličtině. UI texty v angličtině.
