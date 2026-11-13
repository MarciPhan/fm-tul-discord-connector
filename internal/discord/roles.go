package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"sbibolet/internal/config"
	"sbibolet/internal/security"
	"sbibolet/internal/storage"
)

// PerformJoinAndRole automaticky pripoji uzivatele na server a priradi role
func PerformJoinAndRole(discordUserID, userAccessToken string, student *storage.Student) error {
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

	// Urceni roli (jen takovych, ktere na serveru opravdu existuji; jinak by Discord odmitl cely pozadavek)
	serverRoles, rolesErr := guildRoles(guildID)
	if rolesErr != nil {
		return fmt.Errorf("nelze načíst role serveru: %w", rolesErr)
	}
	var rolesToAdd []string
	if id := resolveRoleID(serverRoles, config.GetVerifiedRoleID(), roleNameVerified); id != "" && config.GetVerifiedRoleID() != "" {
		rolesToAdd = append(rolesToAdd, id)
	}

	if student.Role == "Absolvent FM" {
		// Absolvent skolni ucet nema: dostane jen roli absolventa; Student FM mu da az schvaleni spravcem (alumni_approval.go)
		if id := resolveRoleID(serverRoles, "", "Absolvent FM"); id != "" {
			rolesToAdd = append(rolesToAdd, id)
		} else {
			log.Printf("CHYBA: role %q na serveru neexistuje, uživatel %s ji nedostane", "Absolvent FM", discordUserID)
		}
	} else if student.Role == "Zaměstnanec FM" {
		if id := resolveRoleID(serverRoles, config.GetFMStaffRoleID(), roleNameStaff); id != "" {
			rolesToAdd = append(rolesToAdd, id)
		} else {
			log.Printf("CHYBA: role %q na serveru neexistuje, uživatel %s ji nedostane", roleNameStaff, discordUserID)
		}
	} else {
	}

	// Studijni program z vyberu na webu (jen ME, IT, AVI; jine hodnoty se ignoruji)
	programRoleIDs := map[string]string{}
	for _, p := range []string{"ME", "IT", "AVI"} {
		programRoleIDs[p] = resolveRoleID(serverRoles, "", p)
	}
	var programRolesToRemove []string
	if student.Role == "Student FM" {
		if id := programRoleIDs[student.Program]; id != "" {
			rolesToAdd = append(rolesToAdd, id)
			for p, other := range programRoleIDs {
				if p != student.Program && other != "" {
					programRolesToRemove = append(programRolesToRemove, other)
				}
			}
		}
	}

	// Bezpecna ocista jmena pro Discord
	cleanNick := security.SanitizeNickname(student.Name)

	if resp.StatusCode == http.StatusNotFound {
		// Uzivatel neni na serveru -> Pridame ho pres PUT s OAuth2 access tokenem (guilds.join scope)
		log.Printf("Uživatel %s není na serveru, přidávám přes guilds.join...", discordUserID)

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
		if putResp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(putResp.Body, 512))
			log.Printf("CHYBA: přidání uživatele %s na server selhalo (status: %d): %s", discordUserID, putResp.StatusCode, errBody)
			return fmt.Errorf("Discord odmítl přidání na server (status %d)", putResp.StatusCode)
		}
		log.Printf("Uživatel přidán na server (status: %d)", putResp.StatusCode)
		return nil
	}

	// Uzivatel jiz na serveru je -> Pridame role a aktualizujeme prezdivku
	for _, roleID := range rolesToAdd {
		roleURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s/roles/%s", guildID, discordUserID, roleID)
		rReq, _ := http.NewRequest("PUT", roleURL, nil)
		rReq.Header.Set("Authorization", "Bot "+botToken)
		rResp, rErr := client.Do(rReq)
		if rErr != nil {
			log.Printf("CHYBA: přidání role %s uživateli %s selhalo: %v", roleID, discordUserID, rErr)
			continue
		}
		if rResp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(rResp.Body, 512))
			log.Printf("CHYBA: přidání role %s uživateli %s selhalo (status: %d): %s", roleID, discordUserID, rResp.StatusCode, errBody)
		}
		rResp.Body.Close()
	}

	// Zmena oboru: ostatni programove role se odeberou
	for _, roleID := range programRolesToRemove {
		delURL := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s/roles/%s", guildID, discordUserID, roleID)
		dReq, _ := http.NewRequest("DELETE", delURL, nil)
		dReq.Header.Set("Authorization", "Bot "+botToken)
		if dResp, dErr := client.Do(dReq); dErr == nil {
			dResp.Body.Close()
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

