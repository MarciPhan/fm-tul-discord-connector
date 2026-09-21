package discord

import (
	"fmt"
	"log"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// Session je globalni Discord session (pristupna z ostatnich balicku)
var Session *discordgo.Session

// Setup inicializuje a spusti Discord bota
func Setup() {
	if config.Cfg.DiscordToken == "" || config.Cfg.DiscordToken == "SEM_VLOZTE_DISCORD_BOT_TOKEN" {
		log.Println("DISCORD_TOKEN není nastaven v .env – bot není spuštěn (web konektor funguje dál).")
		return
	}

	var err error
	Session, err = discordgo.New("Bot " + config.Cfg.DiscordToken)
	if err != nil {
		log.Printf("Chyba při vytváření bota: %v", err)
		return
	}

	Session.Identify.Intents = discordgo.IntentsAllWithoutPrivileged |
		discordgo.IntentGuildMembers |
		discordgo.IntentGuildMessageReactions |
		discordgo.IntentDirectMessages

	// Ready handler – synchronizace prikazu
	Session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Discord Bot %s je online! Registrováno %d příkazů.", s.State.User.Username, CmdRegistry.CommandCount())
		CmdRegistry.SyncWithGuild(s, config.Cfg.DiscordGuildID)
	})

	// Reakce na zpravy (overeni)
	Session.AddHandler(HandleReactionAdd)

	// Zpracovani slash prikazu
	Session.AddHandler(CmdRegistry.HandleInteraction)

	// Welcome zpravy
	Session.AddHandler(HandleGuildMemberAdd)

	// Audit Log pro nativni mazani a upravy zprav uzivateli
	Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		audit.Log(audit.LevelWarning, "Zpráva smazána", fmt.Sprintf("Byla smazána zpráva s ID `%s` v kanálu <#%s>.", m.ID, m.ChannelID))
	})
	Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		if m.Author != nil { // Mame autora z cache
			audit.Log(audit.LevelInfo, "Zpráva upravena", fmt.Sprintf("Uživatel **%s** upravil zprávu v kanálu <#%s>.", m.Author.Username, m.ChannelID))
		}
	})
	Session.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		audit.Log(audit.LevelWarning, "Uživatel opustil server", fmt.Sprintf("Uživatel **%s** (`%s`) se odpojil ze serveru.", m.User.Username, m.User.ID))
	})

	// Pripojeni k Discord API
	err = Session.Open()
	if err != nil {
		log.Printf("Chyba při připojení bota k Discordu: %v", err)
		return
	}

	// Spusteni Workerů
	StartRSSWorker(Session)
	StartAuditWorker(Session)
}

// Close ukonci Discord session
func Close() {
	StopRSSWorker()
	StopAuditWorker()
	if Session != nil {
		_ = Session.Close()
	}
}

// respondEphemeral odesle ephemeral (soukromou) odpoved na interakci
func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// GetInteractionUser bezpečně získá uživatele z interakce (podporuje server i DM)
func GetInteractionUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i == nil {
		return nil
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	if i.User != nil {
		return i.User
	}
	return nil
}

// GetInteractionUsername bezpečně získá uživatelské jméno
func GetInteractionUsername(i *discordgo.InteractionCreate) string {
	u := GetInteractionUser(i)
	if u != nil {
		return u.Username
	}
	return "Neznámý uživatel"
}

// GetInteractionUserID bezpečně získá ID uživatele
func GetInteractionUserID(i *discordgo.InteractionCreate) string {
	u := GetInteractionUser(i)
	if u != nil {
		return u.ID
	}
	return ""
}
