# NORA Native Client — Testing Checklist

## Confirm dialog pro mazání DM
- [ ] Hover na DM konverzaci v sidebaru → zobrazí se červené X
- [ ] Klik na X → potvrzovací dialog "Delete Conversation"
- [ ] Cancel → dialog se zavře, nic se nesmaže
- [ ] Delete → konverzace zmizí z obou klientů, lokální historie smazána
- [ ] Klik mimo dialog → zavře se

## Typing indikátor
- [ ] Psaní do message inputu → druhý klient vidí "username is typing..."
- [ ] Po 5s bez psaní indikátor zmizí
- [ ] Po odeslání zprávy indikátor zmizí okamžitě
- [ ] Více lidí píše → "user1, user2 are typing..."

## Message edit
- [ ] Hover na vlastní zprávu → tlačítka Reply / Edit / Del
- [ ] Hover na cizí zprávu → jen Reply
- [ ] Klik Edit → text se načte do editoru, nad inputem "Editing message..."
- [ ] Enter → zpráva se uloží, u zprávy se zobrazí "(edited)"
- [ ] X vedle "Editing" → zrušení editace
- [ ] Druhý klient vidí editovanou zprávu okamžitě přes WS

## Message delete
- [ ] Klik Del → potvrzovací dialog "Delete Message"
- [ ] Confirm → zpráva zmizí u všech klientů
- [ ] Cancel → nic se nestane

## Reply
- [ ] Klik Reply → nad inputem se zobrazí "Replying to username: text..."
- [ ] X vedle reply → zrušení
- [ ] Odeslání zprávy s reply → zpráva se zobrazí s citací nad ní (accent bar + text)
- [ ] Reply citace zobrazuje autora a zkrácený text originálu

## Unread indikátory
- [ ] Přijde zpráva do jiného kanálu → kanál se zvýrazní (bílý tučný text + fialová tečka)
- [ ] Klik na kanál → indikátor zmizí
- [ ] Více zpráv do stejného kanálu → stále jen jedna tečka

## Scroll-to-load (starší zprávy)
- [ ] Scroll nahoru až na vrch → automaticky se načtou starší zprávy
- [ ] Scroll pozice zůstane na místě (neskoč na vrch)
- [ ] Pokud nejsou starší zprávy → přestane se pokoušet načítat
- [ ] Přepnutí kanálu → reset stavu (nový kanál zase umí načíst starší)

## Settings panel
- [ ] Klik na * tlačítko dole v sidebaru → otevře se Settings
- [ ] Zobrazuje username a zkrácený public key
- [ ] Sekce "Blocked Users" — seznam blokovaných s Unblock tlačítkem
- [ ] Unblock → uživatel zmizí ze seznamu
- [ ] Žádný pravý panel (members/friends) v settings view

## Friend requesty (z minulé session)
- [ ] Klik "Add Friend" v UserPopup → odešle friend request
- [ ] Druhý klient vidí request v pravém panelu (sekce "REQUESTS")
- [ ] Accept → oba se přidají do friendlistu
- [ ] Decline → request zmizí
- [ ] Uživatel ve friendlistu → UserPopup ukazuje "Remove Friend" místo "Add Friend"

## Timeout/Ban (P2)
- [ ] UserPopup na cizím uživateli (jako owner) → zobrazí "Timeout" a "Ban"
- [ ] UserPopup na sobě → nezobrazí Timeout/Ban
- [ ] UserPopup na ownerovi → nezobrazí Timeout/Ban
- [ ] Jako non-owner → Timeout/Ban se nezobrazí
- [ ] Klik Timeout → dialog s výběrem doby (1min, 5min, 1h, 1d, 7d)
- [ ] Výběr doby → API volání, uživatel dostane timeout
- [ ] Klik mimo timeout dialog → zavře se
- [ ] Klik Ban → potvrzovací dialog "Ban username"
- [ ] Confirm Ban → API volání, uživatel permanentně bannutý
- [ ] Cancel → nic se nestane

## Invite kódy (P2)
- [ ] Settings → sekce "Invites" s tlačítkem "Create"
- [ ] Klik Create → nový invite se vytvoří (10 použití, 1 den)
- [ ] Invite se zobrazí v seznamu s linkem a počtem použití
- [ ] Klik Delete na invite → invite se smaže
- [ ] Settings jsou scrollovatelné (víc obsahu než okno)

## Server Settings (P2, owner only)
- [ ] Jako owner → v Settings se zobrazí sekce "Server Settings"
- [ ] Jako non-owner → sekce se nezobrazí
- [ ] Server Name a Description editory jsou předvyplněné
- [ ] Úprava názvu + klik Save → název se uloží a projeví
- [ ] server.update WS event aktualizuje název v sidebaru
