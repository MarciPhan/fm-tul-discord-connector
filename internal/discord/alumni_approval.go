package discord

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"
)

const (
	alumniApprovePrefix = "alumni_approve:"
	alumniRejectPrefix  = "alumni_reject:"
)

// RequestAlumniApproval posle do audit kanalu zadost o plny pristup absolventa (roli Student FM prideli az spravce)
func RequestAlumniApproval(discordUserID, name string) {
	channel := config.GetAuditChannelID()
	if Session == nil || channel == "" {
		audit.Log(audit.LevelWarning, "Absolvent čeká na schválení", fmt.Sprintf("<@%s> (%s) se ověřil jako absolvent, ale audit kanál není nastavený. Roli Student FM mu přidej ručně.", discordUserID, name))
		return
	}
	_, err := Session.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       "Absolvent čeká na schválení",
			Description: fmt.Sprintf("<@%s> (%s) se ověřil jako absolvent bez školního účtu a má jen roli Absolvent FM. Schválením dostane i Student FM (celý server).", discordUserID, name),
			Color:       embedColor,
			Timestamp:   time.Now().Format(time.RFC3339),
		}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Schválit", Style: discordgo.SuccessButton, CustomID: alumniApprovePrefix + discordUserID},
			discordgo.Button{Label: "Zamítnout", Style: discordgo.DangerButton, CustomID: alumniRejectPrefix + discordUserID},
		}}},
	})
	if err != nil {
		log.Printf("CHYBA: žádost o schválení absolventa %s se nepodařilo odeslat: %v", discordUserID, err)
	}
}

// HandleAlumniApproval zpracuje tlacitka Schvalit / Zamitnout; smi je jen spravce nebo ten, kdo spravuje role
func HandleAlumniApproval(s *discordgo.Session, i *discordgo.InteractionCreate) {
	id := i.MessageComponentData().CustomID
	approve := strings.HasPrefix(id, alumniApprovePrefix)
	if !approve && !strings.HasPrefix(id, alumniRejectPrefix) {
		return
	}
	userID := strings.TrimPrefix(strings.TrimPrefix(id, alumniApprovePrefix), alumniRejectPrefix)

	if i.Member == nil || i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageRoles) == 0 {
		respondEphemeral(s, i, "Tohle smí jen správce.")
		return
	}
	who := GetInteractionUsername(i)

	result := fmt.Sprintf("Zamítnuto (%s). <@%s> zůstává jen Absolvent FM.", who, userID)
	if approve {
		roles, err := guildRoles(config.Cfg.DiscordGuildID)
		roleID := ""
		if err == nil {
			roleID = resolveRoleID(roles, config.GetFMStudentRoleID(), roleNameStudent)
		}
		if roleID == "" {
			respondEphemeral(s, i, "Roli Student FM se nepodařilo najít.")
			return
		}
		if err := s.GuildMemberRoleAdd(config.Cfg.DiscordGuildID, userID, roleID); err != nil {
			log.Printf("CHYBA: schválení absolventa %s selhalo: %v", userID, err)
			respondEphemeral(s, i, "Roli se nepodařilo přidělit (uživatel možná není na serveru, nebo bot nemá oprávnění).")
			return
		}
		result = fmt.Sprintf("Schváleno (%s). <@%s> má Student FM.", who, userID)
		audit.Log(audit.LevelSuccess, "Absolvent schválen", fmt.Sprintf("Správce **%s** schválil absolventa <@%s>.", who, userID))
	} else {
		audit.Log(audit.LevelInfo, "Absolvent zamítnut", fmt.Sprintf("Správce **%s** zamítl plný přístup absolventovi <@%s>.", who, userID))
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{{Title: "Absolvent vyřízen", Description: result, Color: embedColor}},
			Components: []discordgo.MessageComponent{},
		},
	})
}
