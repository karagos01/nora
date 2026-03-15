# Server-wide channel encryption — koncept

## Princip

Zero-knowledge šifrování obsahu kanálů. Server ukládá jen šifrované bloby, klíč nikdy nevidí.

## Distribuce klíče

- Klíč je součástí invite linku: `host:port/code#base64key`
- Fragment (`#...`) se nikdy neposílá na server
- Volitelné heslo na invite — druhá vrstva ochrany (link + heslo)

## Šifrování

- AES-256-GCM (stejně jako groups)
- Formát: `nonce(12B) || ciphertext || tag(16B)`, base64
- Šifruje se: obsah zpráv v kanálech
- Nešifruje se: metadata (timestamps, user IDs, názvy kanálů) — server je potřebuje

## Key rotation (epoch)

- Každý klíč má pořadové číslo (epoch)
- Zpráva nese `epoch` field → klient ví kterým klíčem dešifrovat
- Klient drží keychain: `[{epoch: 1, key: ...}, {epoch: 2, key: ...}]` v `identities.json` per server

### Strategie při rotaci

1. **Soft rotation** — starý klíč zahozen, stará data zůstanou pod starým klíčem. Nová data novým klíčem. Jednoduché.
2. **Hard rotation** — klient stáhne všechno, přešifruje novým klíčem, uploadne zpět. Náročnější, ale čistší.

## Heslo na invite

Dvě možnosti:
- **KDF kombinace**: klíč z invite + heslo → HKDF/PBKDF2 → finální klíč. Samotný link nestačí.
- **Server-side heslo**: heslo chrání přístup na server (server ověří), klíč v linku dešifruje data.

## Stav

Koncept — neimplementováno.
