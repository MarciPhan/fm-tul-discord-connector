package discord

import (
	"log"
	"strings"

	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// HandleSetupCommand zpracuje !setup-overeni prikaz pro odeslani overovaciho embedu
func HandleSetupCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	if strings.TrimSpace(m.Content) != "!setup-overeni" {
		return
	}

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

	sentMsg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{btn},
			},
		},
	})

	if err == nil && sentMsg != nil {
		targetEmoji := config.Cfg.DiscordVerifyEmoji
		if targetEmoji == "" {
			targetEmoji = "🎓"
		}
		_ = s.MessageReactionAdd(m.ChannelID, sentMsg.ID, targetEmoji)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		log.Printf("📢 Ověřovací zpráva s reakcí %s odeslána do kanálu %s (ID zprávy: %s)", targetEmoji, m.ChannelID, sentMsg.ID)
	}
}
