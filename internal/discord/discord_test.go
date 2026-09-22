package discord

import (
	"os"
	"testing"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

func TestIsValidRSSURL(t *testing.T) {
	cases := []struct {
		url   string
		valid bool
	}{
		{"https://tuni.tul.cz/feed/", true},
		{"http://fm.tul.cz/rss.xml", true},
		{"ftp://example.com/feed", false},
		{"file:///etc/passwd", false},
		{"http://localhost:8080/feed", false},
		{"http://127.0.0.1/feed", false},
		{"http://127.0.0.2:8000/", false},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://metadata.google.internal/computeMetadata/v1/", false},
		{"http://10.0.0.1/rss", false},
		{"http://192.168.1.1/rss", false},
		{"http://172.16.0.1/rss", false},
		{"javascript:alert(1)", false},
		{"not-a-url", false},
	}

	for _, tc := range cases {
		err := isValidRSSURL(tc.url)
		if tc.valid && err != nil {
			t.Errorf("Očekávána platná URL pro %s, ale nastala chyba: %v", tc.url, err)
		} else if !tc.valid && err == nil {
			t.Errorf("Očekáváno zamítnutí nebezpečné URL pro %s (SSRF ochrana), ale prošla!", tc.url)
		}
	}
}

func TestGetInteractionUser(t *testing.T) {
	// 1. Nil interaction
	if u := GetInteractionUser(nil); u != nil {
		t.Errorf("Očekáván nil pro nil interakci")
	}

	// 2. DM interakce (Member je nil, User je vyplněn)
	dmInteraction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			User: &discordgo.User{
				ID:       "dm-user-123",
				Username: "student_dm",
			},
		},
	}
	if u := GetInteractionUser(dmInteraction); u == nil || u.ID != "dm-user-123" {
		t.Errorf("Chyba při extrakci uživatele z DM interakce")
	}
	if name := GetInteractionUsername(dmInteraction); name != "student_dm" {
		t.Errorf("Očekáváno jméno student_dm, získáno: %s", name)
	}
	if id := GetInteractionUserID(dmInteraction); id != "dm-user-123" {
		t.Errorf("Očekáváno ID dm-user-123, získáno: %s", id)
	}

	// 3. Guild interakce (Member je vyplněn)
	guildInteraction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Member: &discordgo.Member{
				User: &discordgo.User{
					ID:       "guild-user-456",
					Username: "profesor_guild",
				},
			},
		},
	}
	if u := GetInteractionUser(guildInteraction); u == nil || u.ID != "guild-user-456" {
		t.Errorf("Chyba při extrakci uživatele ze serverové interakce")
	}
	if name := GetInteractionUsername(guildInteraction); name != "profesor_guild" {
		t.Errorf("Očekáváno jméno profesor_guild, získáno: %s", name)
	}
	if id := GetInteractionUserID(guildInteraction); id != "guild-user-456" {
		t.Errorf("Očekáváno ID guild-user-456, získáno: %s", id)
	}
}

func TestAuditWorkerNonBlockingWithoutChannel(t *testing.T) {
	// Ověříme, že i když není nastaven audit kanál, audit log se nezasekne a nezaplní frontu
	_ = config.SetAuditChannelID("")
	defer func() {
		_ = os.Remove(config.ConfigFile)
	}()
	StartAuditWorker(nil)
	defer StopAuditWorker()

	// Odešleme několik auditních zpráv
	for i := 0; i < 10; i++ {
		audit.Log(audit.LevelInfo, "Test Title", "Test Description")
	}

	// Necháme chvilku na odbavení workerem
	time.Sleep(50 * time.Millisecond)

	// Fronta musí být odbavována
	ch := audit.GetChannel()
	if len(ch) > 5 {
		t.Errorf("Fronta auditních zpráv nebyla workerem odbavena (zbývá: %d)", len(ch))
	}
}
