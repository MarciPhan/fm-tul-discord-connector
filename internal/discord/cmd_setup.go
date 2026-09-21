package discord

import (
	"log"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// CmdSetup implementuje /setup prikaz pro odeslani overovaci zpravy s tlacitkem
type CmdSetup struct{}

func (c *CmdSetup) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionAdministrator)
	return &discordgo.ApplicationCommand{
		Name:                     "setup",
		Description:              "Odešle oficiální ověřovací zprávu s tlačítkem (pouze administrátor)",
		DefaultMemberPermissions: &adminPerm,
	}
}

func (c *CmdSetup) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "🎓 Ověření studentů a zaměstnanců FM TUL",
		Description: "Vítejte na Discord serveru Fakulty mechatroniky, informatiky a mezioborových studií TUL!\n\n" +
			"Pro získání přístupu do neveřejných fakultních kanálů a místností **klikněte na reakci 🎓 pod touto zprávou** (nebo na tlačítko níže).\n\n" +
			"Bot vám obratem pošle privátní odkaz k ověření přes univerzitní Microsoft účet (`@tul.cz`).",
		Color: 0x0284c7,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Ověření probíhá jednorázově skrze oficiální univerzitní Microsoft 365.",
		},
	}

	btn := discordgo.Button{
		Label: "Ověřit identitu přes web",
		Style: discordgo.LinkButton,
		URL:   config.Cfg.BaseURL,
	}

	sentMsg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{btn},
			},
		},
	})

	if err != nil {
		respondEphemeral(s, i, "❌ Chyba při odesílání zprávy.")
		return
	}

	targetEmoji := config.Cfg.DiscordVerifyEmoji
	if targetEmoji == "" {
		targetEmoji = "🎓"
	}
	_ = s.MessageReactionAdd(i.ChannelID, sentMsg.ID, targetEmoji)
	
	respondEphemeral(s, i, "✅ Ověřovací zpráva byla odeslána do tohoto kanálu.")
	
	log.Printf("📢 /setup: správce %s odeslal ověřovací zprávu (ID: %s)", i.Member.User.Username, sentMsg.ID)
	audit.Log(audit.LevelInfo, "🛠️ Inicializace serveru", "Správce **"+i.Member.User.Username+"** vytvořil pomocí `/setup` ověřovací bod.")
}

func init() {
	CmdRegistry.Register(&CmdSetup{})
}
