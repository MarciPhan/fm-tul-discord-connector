package discord

import (
	"log"
	"sync"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

var (
	stopAuditChan chan struct{}
	stopAuditOnce sync.Once
)

// StartAuditWorker spouští asynchronní zpracování audit logů z fronty do Discordu
func StartAuditWorker(s *discordgo.Session) {
	stopAuditChan = make(chan struct{})
	eventChan := audit.GetChannel()

	go func() {
		log.Println("Audit Worker spuštěn.")
		for {
			select {
			case evt := <-eventChan:
				auditChannel := config.GetAuditChannelID()
				if auditChannel != "" && s != nil {
					sendAuditEmbed(s, auditChannel, evt)
				} else {
					log.Printf("[AUDIT LOG] %s: %s", evt.Title, evt.Description)
				}
			case <-stopAuditChan:
				log.Println("Audit Worker zastaven.")
				return
			}
		}
	}()
}

// StopAuditWorker bezpečně zastaví zapisování do auditu
func StopAuditWorker() {
	if stopAuditChan != nil {
		stopAuditOnce.Do(func() { close(stopAuditChan) })
	}
}

// sendAuditEmbed sestaví a odešle zprávu do auditního kanálu
func sendAuditEmbed(s *discordgo.Session, auditChannel string, evt audit.Event) {
	var color int

	switch evt.Level {
	case audit.LevelSuccess:
		color = 0x22c55e // Zelená
	case audit.LevelWarning:
		color = 0xf59e0b // Oranžová
	case audit.LevelDanger:
		color = 0xef4444 // Červená
	default:
		color = 0x3b82f6 // Modrá
	}

	embed := &discordgo.MessageEmbed{
		Title:       evt.Title,
		Description: evt.Description,
		Color:       color,
		Timestamp:   evt.Timestamp.Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "TUL Security Audit",
		},
	}

	_, err := s.ChannelMessageSendEmbed(auditChannel, embed)
	if err != nil {
		log.Printf("Nelze odeslat audit log do kanálu %s: %v", auditChannel, err)
	}
}
