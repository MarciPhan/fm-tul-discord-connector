package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"sbibolet/internal/config"
	"sbibolet/internal/security"
	"sbibolet/internal/storage"
)

// PerformJoinAndRole automaticky pripoji uzivatele na server a priradi role
func PerformJoinAndRole(botSession interface{ GetState() string }, discordUserID, userAccessToken string, student *storage.Student) error {
	guildID := config.Cfg.DiscordGuildID
	if guildID == "" {
		return fmt.Errorf("DISCORD_GUILD_ID neni nakonfigurovano")
	}

	botToken := config.Cfg.DiscordToken
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Zjistime, zda uzivatel jiz je na serveru
	memberURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordUserID)
	req, _ := http.NewRequest("GET", memberURL, nil)
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Urceni roli
	var rolesToAdd []string
	if config.Cfg.DiscordVerifiedID != "" {
		rolesToAdd = append(rolesToAdd, config.Cfg.DiscordVerifiedID)
	}
	if student.Role == "Zaměstnanec FM" && config.Cfg.DiscordFMStaffID != "" {
		rolesToAdd = append(rolesToAdd, config.Cfg.DiscordFMStaffID)
	} else if config.Cfg.DiscordFMStudentID != "" {
		rolesToAdd = append(rolesToAdd, config.Cfg.DiscordFMStudentID)
	}

	// Bezpecna ocista jmena pro Discord
	cleanNick := security.SanitizeNickname(student.Name)

	if resp.StatusCode == http.StatusNotFound {
		// Uzivatel neni na serveru -> Pridame ho pres PUT s OAuth2 access tokenem (guilds.join scope)
		log.Printf("➕ Uživatel %s není na serveru, přidávám přes guilds.join...", discordUserID)

		payload := map[string]interface{}{
			"access_token": userAccessToken,
			"roles":        rolesToAdd,
		}
		if cleanNick != "" {
			payload["nick"] = cleanNick
		}
		bodyBytes, _ := json.Marshal(payload)

		putReq, _ := http.NewRequest("PUT", memberURL, bytes.NewBuffer(bodyBytes))
		putReq.Header.Set("Authorization", "Bot "+botToken)
		putReq.Header.Set("Content-Type", "application/json")

		putResp, putErr := client.Do(putReq)
		if putErr != nil {
			return putErr
		}
		defer putResp.Body.Close()
		log.Printf("✅ Uživatel přidán na server (status: %d)", putResp.StatusCode)
		return nil
	}

	// Uzivatel jiz na serveru je -> Pridame role a aktualizujeme prezdivku
	for _, roleID := range rolesToAdd {
		roleURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s/roles/%s", guildID, discordUserID, roleID)
		rReq, _ := http.NewRequest("PUT", roleURL, nil)
		rReq.Header.Set("Authorization", "Bot "+botToken)
		rResp, rErr := client.Do(rReq)
		if rErr == nil {
			rResp.Body.Close()
		}
	}

	// Aktualizace prezdivky
	if cleanNick != "" {
		nickPayload := map[string]string{"nick": cleanNick}
		nickBytes, _ := json.Marshal(nickPayload)
		patchReq, _ := http.NewRequest("PATCH", memberURL, bytes.NewBuffer(nickBytes))
		patchReq.Header.Set("Authorization", "Bot "+botToken)
		patchReq.Header.Set("Content-Type", "application/json")
		pResp, pErr := client.Do(patchReq)
		if pErr == nil {
			pResp.Body.Close()
		}
	}

	return nil
}

// PerformJoinAndRoleDirect – zjednodusena verze bez interface parametru
func PerformJoinAndRoleDirect(discordUserID, userAccessToken string, student *storage.Student) error {
	return PerformJoinAndRole(nil, discordUserID, userAccessToken, student)
}
