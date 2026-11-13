package discord

import "testing"

func TestServerLink(t *testing.T) {
	const invite = "https://discord.gg/RfWG6vYVj2"
	cases := []struct {
		name   string
		member bool
		guild  string
		want   string
	}{
		{"člen serveru dostane přímý odkaz", true, "1544637087779786812", "https://discord.com/channels/1544637087779786812"},
		{"odešel ze serveru, dostane pozvánku", false, "1544637087779786812", invite},
		{"neplatné ID serveru, pozvánka", true, "../evil", invite},
		{"prázdné ID serveru, pozvánka", true, "", invite},
	}
	for _, tc := range cases {
		if got := serverLink(tc.member, tc.guild, invite); got != tc.want {
			t.Errorf("%s: dostal %q, čekal %q", tc.name, got, tc.want)
		}
	}
}
