package discord

import (
	"log"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// CmdSetup implementuje /setup prikaz pro odeslani overovaci zpravy s tlacitky oboru
type CmdSetup struct{}

func (c *CmdSetup) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionAdministrator)
	return &discordgo.ApplicationCommand{
		Name:                     "setup",
		Description:              "Odešle oficiální ověřovací zprávu s tlačítky (pouze administrátor)",
		DefaultMemberPermissions: &adminPerm,
	}
}

// verifyButtons vraci tlacitka overeni: hlavni pro studenty a zamestnance, druhe pro absolventy bez skolniho uctu
func verifyButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.Button{Label: "Ověřit se", Style: discordgo.LinkButton, URL: config.Cfg.PathURL("/go")},
		discordgo.Button{Label: "Jsem absolvent", Style: discordgo.LinkButton, URL: config.Cfg.PathURL("/go?p=ABS")},
	}
}

func (c *CmdSetup) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title:       "Vítej na Discordu FM TUL",
		Description: "Oficiální server Fakulty mechatroniky, informatiky a mezioborových studií TUL.",
		Color:       embedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Jak začít",
				Value: "1. Klikni na **Ověřit se** a přihlas se školním účtem `@tul.cz`.\n" +
					"2. Vyber svůj obor, role se ti přidělí a odemknou se fakultní kanály.\n" +
					"3. Absolvent bez školního účtu klikne na **Jsem absolvent**.",
			},
			{
				Name: "Struktura serveru",
				Value: "**FM | Oficiální informace**: novinky, akce, stáže, zahraničí, soutěže\n" +
					"**FM | Komunita**: tipy na akce, nápady na vylepšení, ztráty a nálezy\n" +
					"**Studentští poradci**: dotazy ke studiu\n" +
					"**Studentské spolky**: Formule, SBC, ESN, TULab, RUN-EU",
			},
			{
				Name:  "Pravidla",
				Value: "Přečti si prosím kanál #pravidla. Ať se ti u nás líbí.",
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Ověření je jednorázové a probíhá přes univerzitní Microsoft 365.",
		},
	}

	sentMsg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: verifyButtons()},
		},
	})
	if err != nil {
		respondEphemeral(s, i, "Chyba při odesílání zprávy.")
		return
	}

	respondEphemeral(s, i, "Ověřovací zpráva byla odeslána do tohoto kanálu.")

	username := GetInteractionUsername(i)
	log.Printf("/setup: správce %s odeslal ověřovací zprávu (ID: %s)", username, sentMsg.ID)
	audit.Log(audit.LevelInfo, "Inicializace serveru", "Správce **"+username+"** vytvořil pomocí `/setup` ověřovací bod.")
}

func init() {
	CmdRegistry.Register(&CmdSetup{})
}
