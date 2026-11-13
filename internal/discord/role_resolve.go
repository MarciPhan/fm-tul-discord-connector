package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// Vychozi nazvy rolí, podle kterych se role najde, kdyz nastavene ID neexistuje
const (
	roleNameStudent  = "Student FM"
	roleNameStaff    = "Zaměstnanec FM"
	roleNameVerified = "Ověřený"
)

// resolveRoleID vrati ID role, ktera na serveru opravdu existuje.
// Nejdriv zkusi nastavene ID, pak roli podle nazvu. Vraci "" kdyz neexistuje ani jedno.
func resolveRoleID(roles []*discordgo.Role, configuredID, name string) string {
	if configuredID != "" {
		for _, r := range roles {
			if r.ID == configuredID {
				return configuredID
			}
		}
		log.Printf("CHYBA KONFIGURACE: role s ID %q (%s) na serveru neexistuje, zkouším najít podle názvu", configuredID, name)
	}
	for _, r := range roles {
		if r.Name == name {
			if configuredID != "" {
				log.Printf("Používám roli %q podle názvu (ID %s) místo neplatného ID %s", name, r.ID, configuredID)
			}
			return r.ID
		}
	}
	return ""
}

// guildRoles nacte role serveru pres aktivni session
func guildRoles(guildID string) ([]*discordgo.Role, error) {
	if Session == nil {
		return nil, fmt.Errorf("Discord session neběží")
	}
	return Session.GuildRoles(guildID)
}

// ValidateRoleConfig pri startu overi, ze role pro studenty a zamestnance na serveru existuji.
// Chyby zapise do logu a do auditniho kanalu, aby se spatna konfigurace nikdy neprojevila potichu.
func ValidateRoleConfig() {
	guildID := config.Cfg.DiscordGuildID
	if guildID == "" {
		return
	}
	roles, err := guildRoles(guildID)
	if err != nil {
		log.Printf("Nelze zkontrolovat role serveru: %v", err)
		return
	}

	problems := ""
	for _, c := range []struct{ id, name, label string }{
		{config.GetFMStudentRoleID(), roleNameStudent, "studenta"},
		{config.GetFMStaffRoleID(), roleNameStaff, "zaměstnance"},
	} {
		if resolveRoleID(roles, c.id, c.name) == "" {
			problems += fmt.Sprintf("- Role %s (`%s`) na serveru neexistuje, ověření ji nepřidělí.\n", c.label, c.name)
		}
	}
	if id := config.GetVerifiedRoleID(); id != "" && resolveRoleID(roles, id, roleNameVerified) == "" {
		problems += fmt.Sprintf("- Role ověřeného (`%s`) na serveru neexistuje.\n", id)
	}

	if problems != "" {
		log.Printf("CHYBA KONFIGURACE ROLÍ:\n%s", problems)
		audit.Log(audit.LevelDanger, "Špatná konfigurace rolí", problems+"Zkontroluj `DISCORD_FM_STUDENT_ID`, `DISCORD_FM_STAFF_ID` a `DISCORD_VERIFIED_ID` v `.env`.")
		return
	}
	log.Println("Konfigurace rolí (Student FM, Zaměstnanec FM) je v pořádku.")
}
