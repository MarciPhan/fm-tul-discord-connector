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
		Title: fmt.Sprintf("Vítej, %s!", userName),
		Description: fmt.Sprintf(
			"%s\n\n"+
				"**Jak získat přístup k fakultním kanálům?**\n"+
				"Klikni na reakci v kanálu pro ověření, nebo použij příkaz `/overit`.\n\n"+
				"Po ověření školním Microsoft účtem (`@tul.cz`) ti budou automaticky přiděleny role.",
			welcomeText,
		),
		Color: 0x10b981,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "FM TUL Discord Server • Automatická uvítací zpráva",
		},
	}

	btn := discordgo.Button{
		Label: "Ověřit identitu přes web",
		Style: discordgo.LinkButton,
		URL:   config.Cfg.BaseURL,
	}

	_, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: fmt.Sprintf("Vítej na serveru, <@%s>!", m.User.ID),
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
