package discord

import (
	"log"

	"sbibolet/internal/audit"

	"github.com/bwmarrin/discordgo"
)

// CmdSpolky implementuje /spolky prikaz pro odeslani zpravy s prihlasenim do studentskych spolku
type CmdSpolky struct{}

func (c *CmdSpolky) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionAdministrator)
	return &discordgo.ApplicationCommand{
		Name:                     "spolky",
		Description:              "Odešle zprávu s přihlášením do studentských spolků přes reakce (pouze administrátor)",
		DefaultMemberPermissions: &adminPerm,
	}
}

func (c *CmdSpolky) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := &discordgo.MessageEmbed{
		Title: "🏁 " + clubMessageTitle,
		Description: "Zareaguj na tuto zprávu a automaticky dostaneš roli daného spolku (odemkne ti jeho kanál). " +
			"Do spolků se můžeš hlásit i odhlašovat podle libosti, sejmutím reakce roli zase ztratíš.\n\n" +
			"🏎️ — Formule\n" +
			"🤝 — SBC\n" +
			"🌍 — ESN\n" +
			"🔬 — TULab\n" +
			"🇪🇺 — RUN-EU Studenti\n\n" +
			"*Reaguj až po dokončení ověření přes školní účet.*",
		Color: embedColor,
	}

	sentMsg, err := s.ChannelMessageSendEmbed(i.ChannelID, embed)
	if err != nil {
		log.Printf("/spolky chyba: %v", err)
		respondEphemeral(s, i, "Chyba při odesílání zprávy.")
		return
	}

	for _, emoji := range []string{"🏎️", "🤝", "🌍", "🔬", "🇪🇺"} {
		if err := s.MessageReactionAdd(i.ChannelID, sentMsg.ID, emoji); err != nil {
			log.Printf("/spolky: nelze přidat reakci %s: %v", emoji, err)
		}
	}

	respondEphemeral(s, i, "Zpráva s přihlášením do spolků byla odeslána do tohoto kanálu.")

	username := GetInteractionUsername(i)
	log.Printf("/spolky: správce %s odeslal přihlášení do spolků (ID: %s)", username, sentMsg.ID)
	audit.Log(audit.LevelInfo, "Přihlášení do spolků", "Správce **"+username+"** vytvořil pomocí `/spolky` zprávu s přihlášením do spolků.")
}

func init() {
	CmdRegistry.Register(&CmdSpolky{})
}
