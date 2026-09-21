package discord

import (
	"fmt"

	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// CmdVerify implementuje /overit prikaz
type CmdVerify struct{}

func (c *CmdVerify) Info() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "overit",
		Description: "Ověření identity studenta/zaměstnance FM TUL a získání rolí",
	}
}

func (c *CmdVerify) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "🎓 Ověření identity FM TUL",
		Description: fmt.Sprintf(
			"Ahoj <@%s>,\n\nPro získání přístupu a rolí na Discordu FM TUL se prosím ověř přes náš webový konektor:\n\n👉 **%s**\n\n1. Přihlásíš se školním Microsoft účtem (`@tul.cz`)\n2. Propojíš svůj Discord účet",
			i.Member.User.ID,
			config.Cfg.BaseURL,
		),
		Color: 0x2563eb,
	}
	btn := discordgo.Button{
		Label: "Přejít na ověření",
		Style: discordgo.LinkButton,
		URL:   config.Cfg.BaseURL,
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}}},
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})
}

func init() {
	CmdRegistry.Register(&CmdVerify{})
}
