# FM TUL ↔ Discord Bridge (Go)

Integrační můstek a Discord bot v jazyce **Go (Golang)** pro automatické ověřování identity studentů a zaměstnanců **Fakulty mechatroniky, informatiky a mezioborových studií (FM TUL)** skrze univerzitní **Microsoft účty (Entra ID / Office 365)**.

Server je přístupný i pro širokou veřejnost (bez ověření). Přihlášení přes TUL slouží k automatickému udělení fakultních rolí a odemčení neveřejných studijních místností.

---

## ⚡ Proč Go?
- **Samostatná binárka:** Není potřeba instalovat Python ani virtuální prostředí, stačí zkompilovat (`go build`).
- **Nízké nároky:** Minimální spotřeba RAM a CPU, ideální pro běh na VPS nebo Raspberry Pi.
- **Rychlost:** Bleskové reakce bota i webového rozhraní.

---

## 🚀 Jak to funguje?

1. **Uživatel na Discordu** napíše slash command `/overit` (nebo `!overit`).
2. Bot vygeneruje privátní (ephemeral) zprávu s tlačítkem obsahujícím jednorázový bezpečnostní token.
3. Uživatel klikne na tlačítko a je přesměrován na **přihlašovací bránu Microsoft TUL** (`login.microsoftonline.com`).
4. Po úspěšném přihlášení univerzitním účtem (`@tul.cz`):
   - Microsoft předá aplikaci autorizační kód.
   - Server si přes Microsoft Graph API (`User.Read`) vyžádá identitu uživatele.
   - Bot na Discordu okamžitě přiřadí roli (**Student FM** / **Zaměstnanec FM**) a volitelně nastaví přezdívku podle reálného jména.
5. Uživatel vidí přehledné potvrzení o spárování účtu.

---

## 🛠️ Instalace a spuštění

### 1. Konfigurace (`.env`)
Zkopírujte `.env.example` do `.env`:
```bash
cp .env.example .env
```

Nastavte hodnoty v `.env`:
- `DISCORD_TOKEN`: Token bota z [Discord Developer Portal](https://discord.com/developers/applications).
- `GUILD_ID`: ID vašeho Discord serveru.
- `FM_STUDENT_ROLE_ID`: ID role pro studenty FM.
- `FM_STAFF_ROLE_ID`: ID role pro zaměstnance FM (volitelné).
- `MICROSOFT_CLIENT_ID`, `MICROSOFT_CLIENT_SECRET`, `MICROSOFT_TENANT_ID`: Údaje, které obdržíte od správy sítě LIANE.

### 2. Spuštění ve vývoji
```bash
go run main.go
```

### 3. Kompilace do produkční binárky
```bash
go build -buildvcs=false -o bot .
./bot
```

---

## 🧪 Vývojářský režim (Simulace / Mock Login)

Nemusíte čekat na vyřízení žádosti u LIANE! Pro lokální testování bota:

1. V `.env` nastavte `DISCORD_TOKEN`, `GUILD_ID` a `FM_STUDENT_ROLE_ID`.
2. Spusťte `./bot` nebo `go run main.go`.
3. Na Discordu napište `/overit` nebo otevřete v prohlížeči `http://localhost:8000`.
4. Zvolte **🧪 Vývojářská simulace (Mock Login)**.
5. Bot vám okamžitě přiřadí roli studenta FM na Discordu.

---

## 📋 Žádost o registraci do TUL tenantu

Text oficiální žádosti pro správu sítě LIANE a garanta z FM naleznete v [LIANE_REQUEST.md](file:///home/marcipan/Dokumenty/skola/sbibolet/LIANE_REQUEST.md).
