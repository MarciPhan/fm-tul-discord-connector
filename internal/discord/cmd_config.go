package discord

import (
	"fmt"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

type CmdConfig struct{}

func (c *CmdConfig) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionAdministrator)
	return &discordgo.ApplicationCommand{
		Name:                     "config",
		Description:              "Dynamická správa konfigurace bota přes Discord (pouze pro administrátory)",
		DefaultMemberPermissions: &adminPerm,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-channel",
				Description: "Nastaví kanál pro vybranou funkci",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "typ",
						Description: "Typ kanálu",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Welcome Channel (Uvítací zprávy)", Value: "welcome"},
							{Name: "Audit Log (Bezpečnostní logy)", Value: "audit"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "kanal",
						Description: "Cílový kanál",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-role",
				Description: "Nastaví roli pro vybranou skupinu",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "typ",
						Description: "Typ role",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Ověřený uživatel (Obecná TUL)", Value: "verified"},
							{Name: "Student FM", Value: "student"},
							{Name: "Zaměstnanec FM", Value: "staff"},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionRole,
						Name:        "role",
						Description: "Cílová role",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-message",
				Description: "Nastaví vlastní uvítací zprávu (podporuje Markdown)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "zprava",
						Description: "Text zprávy",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "view",
				Description: "Zobrazí aktuální stav konfigurace (z JSON a .env)",
			},
		},
	}
}

func (c *CmdConfig) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	subcmd := options[0]
	switch subcmd.Name {
	case "set-channel":
		typ := subcmd.Options[0].StringValue()
		channelID := subcmd.Options[1].ChannelValue(s).ID
		
		var err error
		var msg string
		if typ == "welcome" {
			err = config.SetWelcomeChannelID(channelID)
			msg = fmt.Sprintf("Welcome kanál byl nastaven na <#%s>.", channelID)
		} else if typ == "audit" {
			err = config.SetAuditChannelID(channelID)
			msg = fmt.Sprintf("Audit kanál byl nastaven na <#%s>.", channelID)
		}
		
		if err != nil {
			respondEphemeral(s, i, "Chyba při ukládání: "+err.Error())
		} else {
			respondEphemeral(s, i, msg)
			audit.Log(audit.LevelInfo, "Změna konfigurace", fmt.Sprintf("Admin **%s** změnil %s kanál na <#%s>.", GetInteractionUsername(i), typ, channelID))
		}

	case "set-role":
		typ := subcmd.Options[0].StringValue()
		roleID := subcmd.Options[1].RoleValue(s, i.GuildID).ID
		
		var err error
		var msg string
		if typ == "verified" {
			err = config.SetRoleVerified(roleID)
			msg = fmt.Sprintf("Role pro Ověřené nastavena na <@&%s>.", roleID)
		} else if typ == "student" {
			err = config.SetRoleStudent(roleID)
			msg = fmt.Sprintf("Role pro Studenty FM nastavena na <@&%s>.", roleID)
		} else if typ == "staff" {
			err = config.SetRoleStaff(roleID)
			msg = fmt.Sprintf("Role pro Zaměstnance FM nastavena na <@&%s>.", roleID)
		}

		if err != nil {
			respondEphemeral(s, i, "Chyba při ukládání: "+err.Error())
		} else {
			respondEphemeral(s, i, msg)
			audit.Log(audit.LevelInfo, "Změna konfigurace", fmt.Sprintf("Admin **%s** změnil roli %s na <@&%s>.", GetInteractionUsername(i), typ, roleID))
		}

	case "set-message":
		text := subcmd.Options[0].StringValue()
		err := config.SetWelcomeMessage(text)
		if err != nil {
			respondEphemeral(s, i, "Chyba při ukládání: "+err.Error())
		} else {
			respondEphemeral(s, i, "Uvítací zpráva byla upravena:\n\n"+text)
			audit.Log(audit.LevelInfo, "Změna konfigurace", fmt.Sprintf("Admin **%s** změnil Welcome zprávu.", GetInteractionUsername(i)))
		}

	case "view":
		msg := "**Aktuální konfigurace (Hybrid: JSON + .env)**\n\n"
		msg += formatConfigItem("Welcome Kanál", config.GetWelcomeChannelID(), true)
		msg += formatConfigItem("Audit Kanál", config.GetAuditChannelID(), true)
		msg += formatConfigItem("Role Ověřený", config.GetVerifiedRoleID(), false)
		msg += formatConfigItem("Role Student FM", config.GetFMStudentRoleID(), false)
		msg += formatConfigItem("Role Zaměstnanec FM", config.GetFMStaffRoleID(), false)
		
		msg += "\n**Welcome Zpráva:**\n"
		wMsg := config.GetWelcomeMessage()
		if wMsg == "" {
			msg += "*Nenastaveno (chyba!)*"
		} else {
			msg += "```" + wMsg + "```"
		}
		
		respondEphemeral(s, i, msg)
	}
}

func formatConfigItem(name, id string, isChannel bool) string {
	if id == "" {
		return fmt.Sprintf("- **%s:** *Nenastaveno*\n", name)
	}
	if isChannel {
		return fmt.Sprintf("- **%s:** <#%s>\n", name, id)
	}
	return fmt.Sprintf("- **%s:** <@&%s>\n", name, id)
}

func init() {
	CmdRegistry.Register(&CmdConfig{})
}
