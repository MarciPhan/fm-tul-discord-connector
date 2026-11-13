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
