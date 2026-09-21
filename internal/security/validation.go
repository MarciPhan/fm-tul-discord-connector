package security

import (
	"strings"
	"unicode"
)

// IsValidTULEmail overi, ze email patri striktne pod univerzitu TUL (@tul.cz nebo subdomena)
func IsValidTULEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	domain := parts[1]
	return domain == "tul.cz" || strings.HasSuffix(domain, ".tul.cz")
}

// SanitizeNickname ocisti jmeno od zneuzitelnych znaku na Discordu
func SanitizeNickname(name string) string {
	var b strings.Builder
	for _, r := range name {
		// Odstraneni ridicich znaku
		if unicode.IsControl(r) {
			continue
		}
		// Odstraneni neviditelnych zero-width mezer (ZWS)
		if r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()

	// Odstraneni nebezpecnych tagu a formátovacích znaku
	disallowed := []string{"@everyone", "@here", "<@", "discord.gg", "http://", "https://"}
	for _, d := range disallowed {
		s = strings.ReplaceAll(s, d, "")
	}

	// Normalizace mezer
	s = strings.Join(strings.Fields(s), " ")

	// Orez na limit Discord API (max 32 run)
	runes := []rune(s)
	if len(runes) > 32 {
		runes = runes[:32]
	}
	res := strings.TrimSpace(string(runes))
	if res == "" {
		return "Student FM"
	}
	return res
}

// DetermineRole urcuje, zda jde o studenta ci zamestnance podle pozice a emailu
func DetermineRole(jobTitle, email string) string {
	if jobTitle != "" || strings.Contains(strings.ToLower(email), "zamestnanec") {
		return "Zaměstnanec FM"
	}
	return "Student FM"
}
