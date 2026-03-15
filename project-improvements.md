# TODO

## Prioritní
- [x] CI pipeline (GitHub Actions): `go test`, `go vet`, `go build` pro server + klient
- [x] Integrační testy na kritické handlery (auth, messages, uploads, channels)
- [x] slog rozšířit na všechny handlery (18 souborů, log.Printf jen v main startup)
- [x] WS hub: ochrana proti pomalým klientům (drop counter, disconnect po 10 dropped)

## Výkon
- [ ] SQLite FTS pro message search (teď LIKE)
- [ ] Upload deduplikace (SHA-256 hash → skip pokud existuje)
- [ ] Message idempotency key (ochrana proti duplicitám při reconnectu)

## Bezpečnost
- [ ] govulncheck do CI
- [ ] Timeouty na link preview fetch (OpenGraph)
- [ ] Path traversal audit (game server file explorer, file storage, zip extract)

## Hotové (odškrtnuté z původního auditu)
- [x] RCON IPv6 — net.JoinHostPort
- [x] Strukturované logování — slog zavedeno (částečně)
- [x] Rozsekat messages.go — messages_*.go (pins, emoji, upload, search, ...)
- [x] Rozsekat settings.go — settings_user.go + settings_server.go
- [x] AutoMod spam detekce — implementováno (word filter + spam timeout)
- [x] Device/session management — device fingerprinting + device bans
