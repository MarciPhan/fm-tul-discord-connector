package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"

	"github.com/bwmarrin/discordgo"
)

// CmdDelete implementuje /delete prikaz – bot smaze zpravu
type CmdDelete struct{}

func (c *CmdDelete) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionManageMessages)
	return &discordgo.ApplicationCommand{
		Name:                     "delete",
		Description:              "Bot smaže zprávu podle ID (pouze pro správce)",
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
				Description: "ID zprávy, kterou chceš smazat",
				Required:    true,
			},
		},
	}
}

func (c *CmdDelete) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	var channelID, messageID string
	for _, opt := range options {
		switch opt.Name {
		case "kanal":
			channelID = opt.ChannelValue(s).ID
		case "zprava_id":
			messageID = opt.StringValue()
		}
	}

	if channelID == "" || messageID == "" {
		respondEphemeral(s, i, "❌ Musíš zadat kanál i ID zprávy.")
		return
	}

	err := s.ChannelMessageDelete(channelID, messageID)
	if err != nil {
		log.Printf("⚠️ /delete chyba: %v", err)
		respondEphemeral(s, i, fmt.Sprintf("❌ Nepodařilo se smazat zprávu: %v", err))
		return
	}

	username := GetInteractionUsername(i)
	respondEphemeral(s, i, fmt.Sprintf("✅ Zpráva `%s` v <#%s> byla smazána.", messageID, channelID))
	log.Printf("🗑️ /delete: správce %s smazal zprávu %s v kanálu %s", username, messageID, channelID)
	
	audit.Log(audit.LevelInfo, "🗑️ Příkaz /delete", fmt.Sprintf("Správce **%s** smazal zprávu `%s` v kanálu <#%s>.", username, messageID, channelID))
}

func init() {
	CmdRegistry.Register(&CmdDelete{})
}
