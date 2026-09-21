# AGENTS.md – Pokyny a Konvence pro AI Asistenty

Tento soubor definuje klíčové architektonické invarianty, bezpečnostní pravidla a vývojové postupy pro libovolného AI agenta či asistenta pracujícího v tomto repozitáři.

---

## 1. O projektu & Doménový kontext

- **Projekt:** FM TUL Discord Connector (`sbibolet`).
- **Jazyk & Platforma:** Go (1.21+), bez externích databází (PostgreSQL, MySQL, Redis).
- **Účel:** Bezpečné ověření studentů a zaměstnanců Fakulty mechatroniky, informatiky a mezioborových studií TUL (FM TUL) pomocí univerzitního účtu Microsoft Entra ID a udělení příslušných rolí a přezdívek na Discordu.
- **Klíčové závislosti:** `github.com/bwmarrin/discordgo`, `github.com/joho/godotenv`, `github.com/mmcdole/gofeed`.

---

## 2. Nedotknutelné Invarianty (Architectural Invariants)

1. **Žádné externí databáze:** Veškerá perzistence musí zůstat v lokálních atomicky zapisovaných souborech (`users.json`, `feeds.json`, `discord_config.json`).
2. **Atomický zápis souborů (0600):** Všechny ukládací funkce (`SaveUser`, `SaveDynamic`, `saveFeeds`) MUSÍ zapisovat přes dočasný soubor s právy `0600` a následným `os.Rename`. Při chybě zápisu se temp soubor maže a původní soubor nesmí být poškozen.
3. **Pravidlo 1:1 (Anti-Multi-Accounting):** Jeden univerzitní e-mail / Microsoft ID smí mít svázán právě jeden Discord účet. Validace se provádí atomicky uvnitř `usersMux.Lock()` před zápisem.
4. **Hybridní konfigurace:** `discord_config.json` (dynamická, spravovaná přes `/config`) má vždy přednost před statickou konfigurací z `.env`.
5. **Vše přes Slash příkazy:** Žádné legacy textové prefixové příkazy (např. `!setup`). Všechny administrační i uživatelské funkce jsou registrovány přes Discord Application Slash Commands.

---

## 3. Bezpečnostní pravidla (Security Rules)

- **HMAC Signatury:** Cookie `tul_session` musí mít vždy ověřen digitální podpis `HMAC-SHA256(ID, HMACSecret)` v konstantním čase (`subtle.ConstantTimeCompare`).
- **State Tokeny & PKCE:** Každý OAuth krok vyžaduje unikátní kryptografický state token spotřebovávaný metodami `ConsumeMSALState` a `ConsumeDiscordState` (ochrana proti CSRF a replay útokům).
- **Origin Validation:** Middleware musí validovat hlavičku `Origin` proti normalizované `BASE_URL`.
- **Prevence Nil Pointer Panik:** V handlerech Discordu **nikdy** nepřistupujte přímo k `i.Member.User`! Uživatel mohl vyvolat příkaz v DM, kde je `i.Member` `nil`. Vždy používejte pomocné funkce `GetInteractionUser(i)`, `GetInteractionUsername(i)` a `GetInteractionUserID(i)`.
- **SSRF ochrana v RSS:** Každá URL pro `/rss add` musí projít validací `isValidRSSURL` (blokace `localhost`, privátních IP, cloud metadatových adres `169.254.169.254` a ne-HTTP protokolů).
- **Open Redirect ochrana:** Přesměrování na Discord pozvánku musí být validováno funkcí `isValidDiscordInvite`.

---

## 4. Práce s konkurencí a vlákny (Concurrency Guidelines)

- **Žádné síťové volání pod mutexem:** Nikdy nedržte `sync.Mutex` ani `sync.RWMutex` během HTTP volání (např. při stahování RSS feedů). Vytvořte si snapshot dat pod `RLock()`, uvolněte zámek, proveďte síťové volání s timeoutem a aktualizujte stav pod `Lock()`.
- **Session Thread Safety:** Struktura `session.Session` obsahuje vlastní `Mux sync.RWMutex`. Pro přístup k polím (`Student`, tokeny) používejte dedikované metody (`GetStudent`, `SetStudent`, `ConsumeMSALState`, `ConsumeDiscordState`).

---

## 5. Jak přidat nový Slash příkaz

1. Vytvořte nový soubor v `internal/discord/cmd_<nazev>.go`.
2. Definujte strukturu implementující rozhraní `Command`:
   ```go
   type CmdMujPrikaz struct{}
   func (c *CmdMujPrikaz) Info() *discordgo.ApplicationCommand { ... }
   func (c *CmdMujPrikaz) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) { ... }
   ```
3. Zaregistrujte příkaz v `init()`:
   ```go
   func init() {
       CmdRegistry.Register(&CmdMujPrikaz{})
   }
   ```
4. Pro auditování události zavolejte `audit.Log(audit.LevelInfo, "Název", "Popis")`.
5. Spusťte skript na aktualizaci Knowledge Base: `./scripts/update-knowledge-base.sh`.

---

## 6. Verifikace a Testy

Před odevzdáním změn VŽDY spusťte kompletní testovací sadu s detektorem souběhů:
```bash
go test -v -race -count=1 ./...
```
A ověřte kompilaci binárky:
```bash
go build -buildvcs=false -o fm-tul-bot ./cmd/bot/
```

Dokumentaci udržujte aktuální spuštěním:
```bash
./scripts/update-knowledge-base.sh
```
