package discord

import (
	"log"

	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// Session je globalni Discord session (pristupna z ostatnich balicku)
var Session *discordgo.Session

// Setup inicializuje a spusti Discord bota
func Setup() {
	if config.Cfg.DiscordToken == "" || config.Cfg.DiscordToken == "SEM_VLOZTE_DISCORD_BOT_TOKEN" {
		log.Println("ℹ️ DISCORD_TOKEN není nastaven v .env – bot není spuštěn (web konektor funguje dál).")
		return
	}

	var err error
	Session, err = discordgo.New("Bot " + config.Cfg.DiscordToken)
	if err != nil {
		log.Printf("⚠️ Chyba při vytváření bota: %v", err)
		return
	}

	Session.Identify.Intents = discordgo.IntentsAllWithoutPrivileged |
		discordgo.IntentGuildMembers |
		discordgo.IntentGuildMessageReactions |
		discordgo.IntentDirectMessages

	// Ready handler – synchronizace prikazu
	Session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("🤖 Discord Bot %s je online! Registrováno %d příkazů.", s.State.User.Username, CmdRegistry.CommandCount())
		CmdRegistry.SyncWithGuild(s, config.Cfg.DiscordGuildID)
	})

	// Reakce na zpravy (overeni)
	Session.AddHandler(HandleReactionAdd)

	// Zpracovani slash prikazu
	Session.AddHandler(CmdRegistry.HandleInteraction)

	// Textove prikazy (!setup-overeni)
	Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		HandleSetupCommand(s, m)
	})

	// Welcome zpravy
	Session.AddHandler(HandleGuildMemberAdd)

	// Pripojeni k Discord API
	err = Session.Open()
	if err != nil {
		log.Printf("⚠️ Chyba při připojení bota k Discordu: %v", err)
		return
	}

	// Spusteni RSS Workera
	StartRSSWorker(Session)
}

// Close ukonci Discord session
func Close() {
	StopRSSWorker()
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
