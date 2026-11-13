package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// HandleGuildMemberAdd odesle uvitaci zpravu novemu clenu serveru
func HandleGuildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	audit.Log(audit.LevelInfo, "Nový uživatel na serveru", fmt.Sprintf("Uživatel **%s** (`%s`) se připojil na server.", m.User.Username, m.User.ID))

	channelID := config.GetWelcomeChannelID()
	if channelID == "" {
		return // Welcome zprávy nejsou nakonfigurovány
	}

	userName := m.User.Username
	if m.Nick != "" {
		userName = m.Nick
	}

	welcomeText := config.GetWelcomeMessage()

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Vítej, %s", userName),
		Description: fmt.Sprintf(
			"%s\n\n"+
				"Pro přístup k fakultním kanálům se ověř školním účtem `@tul.cz`. "+
				"Stačí kliknout na tlačítko pod zprávou. Role ti přidělíme automaticky.",
			welcomeText,
		),
		Color: embedColor,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "FM TUL Discord",
		},
	}

	btn := discordgo.Button{
		Label: "Ověřit se školním účtem",
		Style: discordgo.LinkButton,
		URL:   config.Cfg.StartURL(),
	}

	_, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: fmt.Sprintf("<@%s>", m.User.ID),
		Embeds:  []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{btn},
			},
		},
	})

	if err != nil {
		log.Printf("Chyba při odesílání welcome zprávy pro %s: %v", m.User.Username, err)
	} else {
		log.Printf("Welcome zpráva odeslána pro nového člena: %s", m.User.Username)
	}
}
