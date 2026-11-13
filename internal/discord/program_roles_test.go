package discord

import "testing"

func TestProgramRoleName(t *testing.T) {
	cases := []struct {
		emoji string
		want  string
	}{
		{"🔧", "ME"},
		{"💻", "IT"},
		{"🌐", "AVI"},
		{"🎖", "Absolvent FM"},
		{"🎖️", "Absolvent FM"},
		{"🎓", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := programRoleName(tc.emoji); got != tc.want {
			t.Errorf("programRoleName(%q) = %q, očekáváno %q", tc.emoji, got, tc.want)
		}
	}
}

func TestClubRoles(t *testing.T) {
	cases := map[string]string{
		"🏎":  "Formule",
		"🏎️": "Formule",
		"🤝":  "SBC",
		"🌍":  "ESN",
		"🔬":  "TULab",
		"🇪🇺": "RUN-EU Studenti",
	}
	for emoji, want := range cases {
		if got := clubSet.roles[normalizeEmoji(emoji)]; got != want {
			t.Errorf("spolek pro %q = %q, očekáváno %q", emoji, got, want)
		}
	}
	if len(clubSet.exclusive) != 0 {
		t.Error("spolky se nemají vylučovat")
	}
}

func TestIsExclusive(t *testing.T) {
	for _, e := range []string{"🔧", "💻", "🌐"} {
		if !isExclusive(e) {
			t.Errorf("%s má být výlučný program", e)
		}
	}
	if isExclusive("🎖️") {
		t.Error("absolvent nemá být výlučný vůči programům")
	}
}
