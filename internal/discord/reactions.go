package discord

import (
	"fmt"
	"log"
	"sync"
	"time"

	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

var (
	reactionCooldowns    = make(map[string]time.Time)
	reactionCooldownsMux sync.Mutex
)

// IsVerifyReaction rozhoduje, zda dana reakce ma vyvolat proces overeni
func IsVerifyReaction(emojiName, emojiAPI, messageID, botUserID, reactingUserID string) bool {
	if reactingUserID == botUserID {
		return false // Ignorujeme vlastni reakce bota
	}
	if config.Cfg.DiscordVerifyMessageID != "" && messageID != config.Cfg.DiscordVerifyMessageID {
		return false
	}
	targetEmoji := config.Cfg.DiscordVerifyEmoji
	if targetEmoji == "" {
		targetEmoji = "🎓"
	}
	if emojiName != targetEmoji && emojiAPI != targetEmoji {
		if config.Cfg.DiscordVerifyMessageID == "" {
			return false
		}
	}
	return true
}

// HandleReactionAdd zpracuje reakci na zpravu a odesle DM s odkazem na overeni
func HandleReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r == nil {
		return
	}
	botID := ""
	if s != nil && s.State != nil && s.State.User != nil {
		botID = s.State.User.ID
	}
	emojiName := r.Emoji.Name
	emojiAPI := r.Emoji.APIName()
	if !IsVerifyReaction(emojiName, emojiAPI, r.MessageID, botID, r.UserID) {
		return
	}

	// Cooldown kontrola (max 1 reakce za 30 sekund na uzivatele)
	reactionCooldownsMux.Lock()
	now := time.Now()
	// Automatické promazání starých záznamů pro prevenci memory leaku
	if len(reactionCooldowns) > 100 {
		for uID, t := range reactionCooldowns {
			if now.Sub(t) > 5*time.Minute {
				delete(reactionCooldowns, uID)
			}
		}
	}
	lastTime, exists := reactionCooldowns[r.UserID]
	if exists && now.Sub(lastTime) < 30*time.Second {
		reactionCooldownsMux.Unlock()
		_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, emojiAPI, r.UserID)
		return
	}
	reactionCooldowns[r.UserID] = now
	reactionCooldownsMux.Unlock()

	log.Printf("🔔 Uživatel %s kliknul na reakci %s na zprávě %s", r.UserID, emojiName, r.MessageID)

	dmChannel, err := s.UserChannelCreate(r.UserID)
	if err != nil {
		log.Printf("⚠️ Nepodařilo se otevřít DM pro %s: %v", r.UserID, err)
		return
	}

	embed := &discordgo.MessageEmbed{
		Title: "🎓 Ověření identity FM TUL",
		Description: fmt.Sprintf(
			"Ahoj!\n\nKliknul jsi na reakci pro ověření identity na Discord serveru **FM TUL**.\n\n"+
				"Pro propojení účtu a automatické získání rolí klikni na odkaz níže:\n\n"+
				"👉 **%s**\n\n"+
				"1. Přihlásíš se školním Microsoft účtem (`@tul.cz`)\n"+
				"2. Propojíš svůj Discord účet a role ti budou okamžitě přiděleny na serveru.",
			config.Cfg.BaseURL,
		),
		Color: 0x2563eb,
	}

	btn := discordgo.Button{
		Label: "Přejít na ověření",
		Style: discordgo.LinkButton,
		URL:   config.Cfg.BaseURL,
	}

	_, sendErr := s.ChannelMessageSendComplex(dmChannel.ID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{btn},
			},
		},
	})

	if sendErr != nil {
		log.Printf("⚠️ Nelze odeslat DM uživateli %s: %v", r.UserID, sendErr)
		tmpMsg, _ := s.ChannelMessageSend(r.ChannelID, fmt.Sprintf("<@%s> Chtěl jsem ti poslat odkaz k ověření, ale máš zablokované soukromé zprávy (DM). Povol si prosím příjem DM od členů serveru, nebo přejdi přímo na: %s", r.UserID, config.Cfg.BaseURL))
		if tmpMsg != nil {
			go func(cID, mID string) {
				time.Sleep(15 * time.Second)
				_ = s.ChannelMessageDelete(cID, mID)
			}(r.ChannelID, tmpMsg.ID)
		}
	}

	_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.APIName(), r.UserID)
}
