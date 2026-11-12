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

// DetermineRole urcuje, zda jde o studenta ci zamestnance. Studentske ucty v tenantu
// zpravidla nemaji vyplnenou pozici (jobTitle), zamestnanci ji maji (napr. "Ing.",
// "Technik", "Vedouci ustavu"), proto za zamestnance povazujeme kazdeho, kdo ma
// neprazdnou pozici, ktera neobsahuje "student"/"doktorand", nebo employeeType zamestnance.
func DetermineRole(jobTitle, employeeType, email string) string {
	lowerTitle := strings.ToLower(strings.TrimSpace(jobTitle))
	lowerType := strings.ToLower(strings.TrimSpace(employeeType))
	lowerEmail := strings.ToLower(email)

	if lowerType != "" && !strings.Contains(lowerType, "student") {
		return "Zaměstnanec FM"
	}
	if lowerTitle != "" && !strings.Contains(lowerTitle, "student") && !strings.Contains(lowerTitle, "doktorand") {
		return "Zaměstnanec FM"
	}

	// Fallback: kontrola emailove adresy
	if strings.Contains(lowerEmail, "zamestnanec") {
		return "Zaměstnanec FM"
	}
	return "Student FM"
}
