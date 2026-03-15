# NORA v2 — Kompletní soupis features

## Hlavní taháky

### Žádná hesla
- Identita = ed25519 klíčový pár. Privátní klíč šifrovaný AES-GCM heslem lokálně — server ho nikdy nevidí. Challenge-response auth místo klasického loginu.

### E2E šifrované DMs
- ECDH (X25519) + AES-256-GCM. Server relayuje šifrovaný blob, nemůže číst obsah. Discord tohle nemá.

### Ephemeral Groups
- E2E šifrované skupinové konverzace kde server neukládá **žádný obsah zpráv**. Zprávy jen lokálně na klientech. Group key distribuce pairwise, rotace při odchodu člena + counter-based rotace (500 zpráv).

### Self-hosted na 512MB RAM
- Pure Go backend bez CGO, embedded SQLite, žádné externí závislosti (Redis, Postgres...). Běží na nejlevnějším VPS.

### Nativní klient
- Čistý Go + Gio UI framework. GPU renderovaný (Direct3D/OpenGL/Vulkan), žádný browser engine, žádné WebView. Cross-compile z Linuxu pro Windows.

### AGPL-3.0 + source endpoint
- Server má `GET /api/source` co vrátí celý zdroják jako tarball. Plný AGPL compliance.

---

## Komunikace

- **Textové kanály** — replies (citace), reactions (emoji toggle), pins, edit/delete, typing indicator
- **Message grouping** — zprávy od stejného uživatele se vizuálně sloučí (Discord-style)
- **Voice kanály** — WebRTC P2P mesh, Google STUN, VAD (auto-mute), speaking indikátor, per-peer volume, mic VU meter, gain/volume slidery
- **Screen sharing** — H.264 enkódování přes ffmpeg, WebRTC data channel, low-latency viewer
- **1:1 DM hovory** — ring animace, 30s timeout, accept/decline
- **DM file transfer** — WebRTC data channel, peer-to-peer
- **Custom server emoji** — upload PNG/GIF/WebP, `:name:` syntax, real-time sync přes WS
- **Webhook / Bot API** — `POST /api/webhooks/{id}/{token}` pro externí služby (GitHub, CI/CD, monitoring)

---

## UX detaily

- **Deterministické barvy jmen** — hash username → konzistentní HSL barva
- **24h formát času** — grouped zprávy ukazují čas při hoveru
- **Unread badges** — DMs i groups (99+)
- **Notification sounds** — cross-platform (paplay/aplay/PowerShell/afplay)
- **Voice join/leave zvuky** — ascending/descending tóny
- **Audio device selection** — výběr mikrofonu i reproduktoru
- **Unicode emoji picker** — 6 kategorií + custom server emoji
- **Inline image preview** — scale+clip, click-to-open v browseru
- **Message formatting** — bold, italic, code, code blocks, links, custom emoji
- **Mention autocomplete** — @username

---

## Moderace & Role

- **Bitmask permissions** — 12 permissions (Send, Read, ManageMessages, ManageChannels, ManageRoles, ManageInvites, Kick, Ban, Upload, Admin, ManageEmojis, ViewActivity)
- **Role hierarchie** — nižší position = vyšší rank, nemůžeš grantovat co nemáš
- **Timeout** — dočasný ban (1min–7dní) s důvodem
- **Ban** — permanentní + IP ban
- **Block** — jednosměrný (druhý o tom neví), blokuje DM i friend requesty
- **Audit log** — historie admin akcí

---

## Kategorie kanálů

- Barevné boxy s levým border (`#RRGGBB`)
- Drag-and-drop reorder kanálů mezi kategoriemi
- Text i voice kanály ve stejné kategorii

---

## Bezpečnost

- **Žádný password hash na serveru** — challenge-response, server ukládá jen public key
- **E2E DMs** — ECDH → HKDF-SHA256 → AES-256-GCM
- **E2E Groups** — symetrický group key, pairwise distribuce, key rotation (counter + member leave)
- **Pending messages** — offline DM delivery, auto-cleanup po 30 dnech
- **IP ban** — kontrola při registraci
- **PBKDF2 800k iterací** — šifrování privátního klíče
- **SHA-256 checksums** — auto-update verifikace integrity
- **Path traversal ochrana** — SafePath + prefix validace
- **Rate limiting** — globální + per-user upload limiter
- **TLS 1.2+** — minimální TLS verze v klientu

---

## Technické zajímavosti

- **5 Go závislostí serveru** — websocket, sqlite, jwt, uuid, toml
- **SQLite WAL mode** — optimalizovaný pro concurrent reads
- **WebSocket hub** — typed events, per-user broadcast, voice state tracking, auto-leave cleanup
- **JWT 15min + refresh 7d** — token rotation, instant revoke při logout
- **Chunked resumable upload** — 256KB chunky, pause/resume, 30min session TTL
- **Multi-server klient** — per-server connection, identity per keypair
- **Auto-update** — version.json manifest s SHA-256, build number comparison

---

## Přílohy & Upload

- **Chunked resumable upload** — pause/resume, progress bar
- **Konfigurovatelný limit** — adminem v nora.toml
- **Inline image preview** — obrázky přímo v chatu
- **Media galerie** — prohlížeč všech nahraných souborů na serveru
- **Sdílený file storage** — složky s permissions, upload/download

---

## Další features

- **Game server management** — Docker kontejnery, TOML config, file explorer + text editor, 5 presetů (Minecraft, Valheim, Factorio, Terraria, CS2)
- **LAN Party** — WireGuard VPN, automatická IP alokace, keypair generace
- **Sdílené adresáře** — P2P file sharing, FUSE mount (Linux), WebDAV, permissions per uživatel
- **Video přehrávač** — ffmpeg backend, inline v klientu
- **P2P file transfer** — WebRTC data channel

---

## NORA vs Discord

| | NORA | Discord |
|---|---|---|
| Hesla | ed25519 klíč | email + heslo |
| E2E šifrované DMs | ano | ne |
| Server vidí obsah | ne (DM/groups) | ano |
| Self-hosted | ano | ne |
| Open source | AGPL-3.0 | ne |
| Min. RAM | 512MB | N/A |
| Upload limit | konfigurovatelný | 25MB free / 500MB Nitro |
| Custom emoji | zdarma | Nitro |
| Voice | P2P mesh | centralizovaný |
| Ephemeral groups | ano | ne |
| Game servers | ano | ne |
| VPN (LAN Party) | ano | ne |
| File sharing P2P | ano | ne |
