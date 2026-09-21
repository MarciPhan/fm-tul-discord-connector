# Žádost o registraci aplikace v Microsoft Entra ID (TUL Tenant) pro FM Discord Bridge

**Komu:** Správa sítě LIANE (info@tul.cz)  
**Předmět:** Žádost o registraci aplikace (Microsoft OAuth) pro fakultní Discord server FM TUL  

---

Dobrý den,

obracím se na Vás ve spolupráci s garantem projektu [Jméno garanta – zaměstnance FM] s žádostí o registraci nové webové aplikace v univerzitním tenantu Microsoft Entra ID (Azure AD).

Aplikace slouží k jednorázovému ověření identity studentů a zaměstnanců Fakulty mechatroniky, informatiky a mezioborových studií (FM) pro automatické přidělení rolí na fakultním Discord serveru.

### Technické specifikace aplikace

- **Název aplikace:** FM TUL Discord Bridge
- **Typ aplikace:** Webová aplikace (Web Application)
- **Podporované typy účtů:** Pouze účty v tomto organizačním adresáři (TUL tenant – Single tenant)
- **Redirect URIs (Reply URLs):**
  - Vývojové: `http://localhost:8000/msal`
  - Produkční: `https://[bude_doplneno_pri_nasazeni]/msal`

### Požadovaná API oprávnění (Microsoft Graph)
Pro zjištění totožnosti uživatele požadujeme pouze standardní delegovaná oprávnění (Delegated permissions):
1. `User.Read` – pro načtení základního profilu uživatele (jméno, e-mail, identifikátor, pracovní pozice / obor)
2. `openid`, `profile`, `email`

### Cílová skupina a řízení přístupu
V souladu s Vaší nabídkou žádáme o **omezení přístupu k aplikaci v tenantu pouze na členy FM (studenti a zaměstnanci FM)**. Uživatelé z jiných fakult by neměli mít možnost aplikaci autorizovat.

### Životní cyklus a nakládání s daty
- Ověření probíhá jednorázově při prvním vstupu studenta/zaměstnance na Discord.
- Aplikace neukládá žádná citlivá hesla, pouze jednorázově spáruje ID Discord účtu se statusem člena FM.

### Garant projektu (zaměstnanec TUL)
- **Jméno a příjmení:** [Doplňte jméno garanta, např. Ing. Jan Novák, Ph.D.]
- **Katedra / Pracoviště:** [např. Katedra mechatroniky / FM TUL]
- **E-mail:** [garant@tul.cz]

---

Po vytvoření registrace aplikace prosíme o zaslání následujících údajů pro konfiguraci:
- **Application (client) ID**
- **Directory (tenant) ID**
- **Client Secret (Value)**

Předem velmi děkujeme za spolupráci a vyřízení žádosti.

S pozdravem,

[Vaše Jméno]  
[Váš ročník / obor na FM TUL]  
[Váš kontakt / e-mail]
