package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"

	"github.com/bwmarrin/discordgo"
)

// CmdSay implementuje /say prikaz – bot odesle zpravu do zvoleneho kanalu
type CmdSay struct{}

func (c *CmdSay) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionManageMessages)
	return &discordgo.ApplicationCommand{
		Name:                     "say",
		Description:              "Bot odešle zprávu do zvoleného kanálu (pouze pro správce)",
		DefaultMemberPermissions: &adminPerm,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionChannel,
				Name:        "kanal",
				Description: "Kanál, do kterého má bot zprávu odeslat",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "zprava",
				Description: "Text zprávy, kterou bot odešle",
				Required:    true,
			},
		},
	}
}

func (c *CmdSay) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	var channelID, message string
	for _, opt := range options {
		switch opt.Name {
		case "kanal":
			channelID = opt.ChannelValue(s).ID
		case "zprava":
			message = opt.StringValue()
		}
	}

	if channelID == "" || message == "" {
		respondEphemeral(s, i, "❌ Musíš zadat kanál i zprávu.")
		return
	}

	sentMsg, err := s.ChannelMessageSend(channelID, message)
	if err != nil {
		log.Printf("⚠️ /say chyba: %v", err)
		respondEphemeral(s, i, fmt.Sprintf("❌ Nepodařilo se odeslat zprávu: %v", err))
		return
	}

	username := GetInteractionUsername(i)
	respondEphemeral(s, i, fmt.Sprintf("✅ Zpráva odeslána do <#%s> (ID: `%s`)", channelID, sentMsg.ID))
	log.Printf("📢 /say: správce %s odeslal zprávu do kanálu %s", username, channelID)

	audit.Log(audit.LevelInfo, "📢 Příkaz /say", fmt.Sprintf("Správce **%s** odeslal zprávu do kanálu <#%s>.\n**Obsah:** %s", username, channelID, message))
}

func init() {
	CmdRegistry.Register(&CmdSay{})
}
