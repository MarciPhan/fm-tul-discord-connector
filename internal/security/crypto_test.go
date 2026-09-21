package security

import (
	"strings"
	"testing"
)

func TestGenerateID(t *testing.T) {
	id := GenerateID()
	if len(id) != 32 {
		t.Errorf("Očekáváno 32 znaků, získáno %d", len(id))
	}
}

func TestGenerateStateToken(t *testing.T) {
	token := GenerateStateToken()
	if len(token) != 64 {
		t.Errorf("Očekáváno 64 znaků, získáno %d", len(token))
	}
}

func TestPKCE(t *testing.T) {
	verifier, challenge := GeneratePKCE()
	if len(verifier) < 43 {
		t.Errorf("Verifier musí mít alespoň 43 znaků, má %d", len(verifier))
	}
	if challenge == "" {
		t.Error("Challenge nesmí být prázdná")
	}
}

func TestConstantTimeCompare(t *testing.T) {
	if !ConstantTimeCompare("secret123", "secret123") {
		t.Error("Shodné řetězce musí vrátit true")
	}
	if ConstantTimeCompare("secret123", "secret124") {
		t.Error("Rozdílné řetězce musí vrátit false")
	}
}

func TestIsValidTULEmail(t *testing.T) {
	valid := []string{"jan@tul.cz", "student@fm.tul.cz", "A@TUL.CZ"}
	invalid := []string{"utocnik@gmail.com", "fake@seznam.cz", "tul.cz@hacker.com", ""}

	for _, e := range valid {
		if !IsValidTULEmail(e) {
			t.Errorf("Očekáváno validní: %s", e)
		}
	}
	for _, e := range invalid {
		if IsValidTULEmail(e) {
			t.Errorf("Očekáváno nevalidní: %s", e)
		}
	}
}

func TestSanitizeNickname(t *testing.T) {
	dangerous := "Jan @everyone Novák <@12345> discord.gg/cheat http://evil.com"
	clean := SanitizeNickname(dangerous)
	if strings.Contains(clean, "@everyone") || strings.Contains(clean, "discord.gg") || strings.Contains(clean, "http://") {
		t.Errorf("Přezdívka obsahuje zakázané tagy: %s", clean)
	}

	if SanitizeNickname("Jan Novák") != "Jan Novák" {
		t.Errorf("Běžné jméno nesmí být změněno")
	}
}
