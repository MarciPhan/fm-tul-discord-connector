package security

import (
	"encoding/base64"
	"encoding/json"
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

// IDTokenRoles vrati hodnoty claimu "roles" z ID tokenu. Token prijde primo z token
// endpointu Microsoftu po TLS (code flow), proto podpis neověřujeme (OIDC Core 3.1.3.7).
func IDTokenRoles(idToken string) []string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Roles []string `json:"roles"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims.Roles
}

// DetermineRole urcuje, zda jde o studenta ci zamestnance. Prednost ma claim "roles"
// z Entra ID (TUL predava role employee / student). Jako zaloha slouzi profil z Graph:
// studentske ucty zpravidla nemaji vyplnenou pozici (jobTitle), zamestnanci ji maji,
// proto za zamestnance povazujeme kazdeho s neprazdnou pozici bez "student"/"doktorand"
// nebo s employeeType zamestnance.
func DetermineRole(tokenRoles []string, jobTitle, employeeType, email string) string {
	for _, r := range tokenRoles {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case "employee", "staff", "zamestnanec", "zaměstnanec":
			return "Zaměstnanec FM"
		}
	}
	for _, r := range tokenRoles {
		if strings.EqualFold(strings.TrimSpace(r), "student") {
			return "Student FM"
		}
	}

	lowerTitle := strings.ToLower(strings.TrimSpace(jobTitle))
	lowerType := strings.ToLower(strings.TrimSpace(employeeType))
	lowerEmail := strings.ToLower(email)

	if lowerType != "" && !strings.Contains(lowerType, "student") {
		return "Zaměstnanec FM"
	}
	if lowerTitle != "" && !strings.Contains(lowerTitle, "student") && !strings.Contains(lowerTitle, "doktorand") {
		return "Zaměstnanec FM"
	}
	if strings.Contains(lowerEmail, "zamestnanec") {
		return "Zaměstnanec FM"
	}
	return "Student FM"
}
