package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestResolveRoleID(t *testing.T) {
	roles := []*discordgo.Role{
		{ID: "111", Name: "Student FM"},
		{ID: "222", Name: "Zaměstnanec FM"},
	}
	cases := []struct {
		name, configured, roleName, want string
	}{
		{"platné ID", "111", "Student FM", "111"},
		{"zástupné ID padá na název", "123456789012345678", "Student FM", "111"},
		{"prázdné ID padá na název", "", "Zaměstnanec FM", "222"},
		{"neexistuje ani ID ani název", "999", "Ověřený", ""},
		{"prázdné ID a neexistující název", "", "Ověřený", ""},
	}
	for _, tc := range cases {
		if got := resolveRoleID(roles, tc.configured, tc.roleName); got != tc.want {
			t.Errorf("%s: resolveRoleID(%q, %q) = %q, očekáváno %q", tc.name, tc.configured, tc.roleName, got, tc.want)
		}
	}
}
