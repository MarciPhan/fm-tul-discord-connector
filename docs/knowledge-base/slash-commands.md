# Registr a Přehled Slash Příkazů (Discord)

Dokument popisuje architekturu rozšiřitelného registru příkazů (`CmdRegistry`) a podrobnou specifikaci všech existujících Discord slash příkazů.

---

## 1. Architektura registru (`internal/discord/registry.go`)

Každý příkaz implementuje rozhraní `Command`:

```go
type Command interface {
    Info() *discordgo.ApplicationCommand
    Handle(s *discordgo.Session, i *discordgo.InteractionCreate)
}
```

- **Automatická registrace:** Příkazy se registrují ve svých balíčcích pomocí funkce `init()`:
  ```go
  func init() {
      CmdRegistry.Register(&CmdSetup{})
  }
  ```
- **Synchronizace se serverem:** Při startu (`Ready` event) funkce `CmdRegistry.SyncWithGuild(s, guildID)` automaticky zaregistruje všechny příkazy do vybraného Discord serveru.
- **Bezpečná obsluha uživatele:**
  - Vždy se používají pomocné funkce `GetInteractionUser(i)`, `GetInteractionUsername(i)` a `GetInteractionUserID(i)`.
  - Zabraňuje pádu na `nil pointer dereference`, pokud je příkaz vyvolán v soukromé zprávě (DM).

---

## 2. Katalog příkazů

### 2.1 Administrační příkazy

| Příkaz | Oprávnění | Popis | Podpříkazy / Volby |
| :--- | :--- | :--- | :--- |
| `/setup` | Administrátor | Odešle oficiální uvítací ověřovací zprávu s tlačítkem a reakcí. | *Žádné* |
| `/config set-channel` | Administrátor | Dynamicky nastaví cílový kanál pro uvítání nebo audit log. | `typ`: `welcome` \| `audit`<br>`kanal`: kanál |
| `/config set-role` | Administrátor | Dynamicky nastaví cílovou roli. | `typ`: `verified` \| `student` \| `staff`<br>`role`: role |
| `/config set-message` | Administrátor | Nastaví text uvítací zprávy. | `zprava`: string (podpora Markdownu) |
| `/config view` | Administrátor | Zobrazí přehled aktuální hybridní konfigurace. | *Žádné* |
| `/say` | Správce zpráv | Odešle zprávu jménem bota do vybraného kanálu. | `kanal`: kanál<br>`zprava`: string |
| `/edit` | Správce zpráv | Upraví zprávu odeslanou botem. | `kanal`: kanál<br>`zprava_id`: ID zprávy<br>`novy_text`: string |
| `/delete` | Správce zpráv | Smaže zprávu podle jejího ID. | `kanal`: kanál<br>`zprava_id`: ID zprávy |
| `/rss add` | Správce zpráv | Přidá nový RSS feed (chráněno proti SSRF). | `url`: HTTP/HTTPS URL<br>`kanal`: kanál |
| `/rss list` | Správce zpráv | Vypíše seznam aktivně sledovaných RSS feedů. | *Žádné* |
| `/rss remove` | Správce zpráv | Odebere sledovaný RSS feed. | `id`: ID feedu |

### 2.2 Veřejné uživatelské příkazy

| Příkaz | Oprávnění | Popis |
| :--- | :--- | :--- |
| `/overit` | Všichni uživatelé | Zašle uživateli soukromou (ephemeral) zprávu s odkazem na webový ověřovací portál. |
