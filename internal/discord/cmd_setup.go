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

// verifyButtons vraci tlacitka overeni: kazde rovnou spusti overeni s ulozenym oborem
func verifyButtons() []discordgo.MessageComponent {
	link := func(label, path string) discordgo.MessageComponent {
		return discordgo.Button{Label: label, Style: discordgo.LinkButton, URL: config.Cfg.PathURL(path)}
	}
	return []discordgo.MessageComponent{
		link("Ověřit se: ME", "/go?p=ME"),
		link("Ověřit se: IT", "/go?p=IT"),
		link("Ověřit se: AVI", "/go?p=AVI"),
		link("Ověřit se: zaměstnanec", "/go"),
		link("Ověřit se: absolvent", "/go?p=ABS"),
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
				Value: "1. Klikni na tlačítko **Ověřit se** podle svého oboru: **ME**, **IT**, **AVI**, nebo **zaměstnanec**. Přihlásíš se školním účtem `@tul.cz`.\n" +
					"2. Tím jsi ověřen a rovnou dostaneš role. Odemknou se ti fakultní kanály.\n" +
					"3. Absolvent bez školního účtu zvolí **Ověřit se: absolvent**.",
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
