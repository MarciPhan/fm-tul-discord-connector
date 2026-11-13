package discord

import (
	"log"

	"sbibolet/internal/audit"

	"github.com/bwmarrin/discordgo"
)

// CmdProgramy implementuje /programy prikaz pro odeslani zpravy s vyberem studijniho programu
type CmdProgramy struct{}

func (c *CmdProgramy) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionAdministrator)
	return &discordgo.ApplicationCommand{
		Name:                     "programy",
		Description:              "Odešle zprávu s výběrem studijního programu přes reakce (pouze administrátor)",
		DefaultMemberPermissions: &adminPerm,
	}
}

func (c *CmdProgramy) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "🎯 " + programMessageTitle,
		Description: "Zvol reakcí svůj studijní program (kdykoliv ho můžeš změnit opětovnou reakcí):\n\n" +
			"🔧 — Mechatronika (ME)\n" +
			"💻 — Informační technologie (IT)\n" +
			"🌐 — Aplikované vědy v inženýrství (AVI)\n\n" +
			"🎖️ — Jsem absolvent/absolventka FM\n\n" +
			"*Reaguj až po dokončení ověření přes školní účet.*",
		Color: embedColor,
	}

	sentMsg, err := s.ChannelMessageSendEmbed(i.ChannelID, embed)
	if err != nil {
		log.Printf("/programy chyba: %v", err)
		respondEphemeral(s, i, "Chyba při odesílání zprávy.")
		return
	}

	for _, emoji := range []string{"🔧", "💻", "🌐", "🎖️"} {
		if err := s.MessageReactionAdd(i.ChannelID, sentMsg.ID, emoji); err != nil {
			log.Printf("/programy: nelze přidat reakci %s: %v", emoji, err)
		}
	}

	respondEphemeral(s, i, "Zpráva s výběrem programu byla odeslána do tohoto kanálu.")

	username := GetInteractionUsername(i)
	log.Printf("/programy: správce %s odeslal výběr programu (ID: %s)", username, sentMsg.ID)
	audit.Log(audit.LevelInfo, "Výběr programu", "Správce **"+username+"** vytvořil pomocí `/programy` zprávu s výběrem studijního programu.")
}

func init() {
	CmdRegistry.Register(&CmdProgramy{})
}
