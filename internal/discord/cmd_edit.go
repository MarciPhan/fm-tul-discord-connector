package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"

	"github.com/bwmarrin/discordgo"
)

// CmdEdit implementuje /edit prikaz – bot upravi vlastni zpravu
type CmdEdit struct{}

func (c *CmdEdit) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionManageMessages)
	return &discordgo.ApplicationCommand{
		Name:                     "edit",
		Description:              "Bot upraví svou vlastní zprávu (pouze pro správce)",
		DefaultMemberPermissions: &adminPerm,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "kanal",
				Description: "Kanál, ve kterém se zpráva nachází",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "zprava_id",
				Description: "ID zprávy, kterou chceš upravit",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "novy_text",
				Description: "Nový obsah zprávy",
				Required:    true,
			},
		},
	}
}

func (c *CmdEdit) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	var channelID, messageID, newText string
	for _, opt := range options {
		switch opt.Name {
		case "kanal":
			channelID = opt.ChannelValue(s).ID
		case "zprava_id":
			messageID = opt.StringValue()
		case "novy_text":
			newText = opt.StringValue()
		}
	}

	if channelID == "" || messageID == "" || newText == "" {
		respondEphemeral(s, i, "❌ Musíš zadat kanál, ID zprávy i nový text.")
		return
	}

	// Overime, ze zprava patri botovi
	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil {
		respondEphemeral(s, i, fmt.Sprintf("❌ Zprávu `%s` nelze najít v kanálu <#%s>.", messageID, channelID))
		return
	}
	if msg.Author.ID != s.State.User.ID {
		respondEphemeral(s, i, "❌ Tuto zprávu bot nemůže upravit – není její autor.")
		return
	}

	_, err = s.ChannelMessageEdit(channelID, messageID, newText)
	if err != nil {
		log.Printf("⚠️ /edit chyba: %v", err)
		respondEphemeral(s, i, fmt.Sprintf("❌ Nepodařilo se upravit zprávu: %v", err))
		return
	}

	username := GetInteractionUsername(i)
	respondEphemeral(s, i, fmt.Sprintf("✅ Zpráva `%s` v <#%s> byla úspěšně upravena.", messageID, channelID))
	log.Printf("✏️ /edit: správce %s upravil zprávu %s v kanálu %s", username, messageID, channelID)

	audit.Log(audit.LevelInfo, "✏️ Příkaz /edit", fmt.Sprintf("Správce **%s** upravil zprávu `%s` v kanálu <#%s>.\n**Nový text:** %s", username, messageID, channelID, newText))
}

func init() {
	CmdRegistry.Register(&CmdEdit{})
}
