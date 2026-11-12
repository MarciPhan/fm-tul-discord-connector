package security

import (
	"encoding/base64"
	"testing"
)

func TestDetermineRole(t *testing.T) {
	cases := []struct {
		roles            []string
		title, typ, mail string
		want             string
	}{
		{nil, "", "", "jan.novak@tul.cz", "Student FM"},
		{nil, "Student", "", "jan.novak@tul.cz", "Student FM"},
		{nil, "Doktorand", "", "jan.novak@tul.cz", "Student FM"},
		{nil, "Ing.", "", "hynek.vaclav.svobodny@tul.cz", "Zaměstnanec FM"},
		{nil, "", "Employee", "a.b@tul.cz", "Zaměstnanec FM"},
		{[]string{"employee"}, "", "", "a.b@tul.cz", "Zaměstnanec FM"},
		{[]string{"student"}, "", "", "a.b@tul.cz", "Student FM"},
		{[]string{"student"}, "Technik", "", "a.b@tul.cz", "Student FM"},
		{[]string{"student", "employee"}, "", "", "a.b@tul.cz", "Zaměstnanec FM"},
	}
	for _, c := range cases {
		if got := DetermineRole(c.roles, c.title, c.typ, c.mail); got != c.want {
			t.Errorf("DetermineRole(%v,%q,%q,%q)=%q, chci %q", c.roles, c.title, c.typ, c.mail, got, c.want)
		}
	}
}

func TestIDTokenRoles(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"roles":["employee"]}`))
	tok := "x." + payload + ".y"
	if got := IDTokenRoles(tok); len(got) != 1 || got[0] != "employee" {
		t.Errorf("IDTokenRoles=%v", got)
	}
	if IDTokenRoles("nejde.o.token") != nil || IDTokenRoles("") != nil {
		t.Error("neplatný token má vrátit nil")
	}
}
