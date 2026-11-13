package discord

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"sbibolet/internal/config"
)

var snowflake = regexp.MustCompile(`^[0-9]{5,25}$`)

// IsGuildMember se u Discordu zeptá (botem), jestli uživatel právě teď je na serveru.
// Při chybě vrací false, takže se použije pozvánka.
func IsGuildMember(discordUserID string) bool {
	guildID := config.Cfg.DiscordGuildID
	if !snowflake.MatchString(guildID) || !snowflake.MatchString(discordUserID) {
		return false
	}

	url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, discordUserID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bot "+config.Cfg.DiscordToken)

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ServerLink vrátí přímý odkaz na server, pokud na něm uživatel je, jinak pozvánku.
func ServerLink(discordUserID, inviteURL string) string {
	return serverLink(IsGuildMember(discordUserID), config.Cfg.DiscordGuildID, inviteURL)
}

func serverLink(isMember bool, guildID, inviteURL string) string {
	if isMember && snowflake.MatchString(guildID) {
		return "https://discord.com/channels/" + guildID
	}
	return inviteURL
}
