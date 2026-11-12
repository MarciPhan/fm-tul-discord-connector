package security

import "testing"

func TestDetermineRole(t *testing.T) {
	cases := []struct{ title, typ, email, want string }{
		{"", "", "jan.novak@tul.cz", "Student FM"},
		{"Student", "", "jan.novak@tul.cz", "Student FM"},
		{"Doktorand", "", "jan.novak@tul.cz", "Student FM"},
		{"Ing.", "", "hynek.vaclav.svobodny@tul.cz", "Zaměstnanec FM"},
		{"Technik", "", "a.b@tul.cz", "Zaměstnanec FM"},
		{"", "Employee", "a.b@tul.cz", "Zaměstnanec FM"},
		{"", "Student", "a.b@tul.cz", "Student FM"},
	}
	for _, c := range cases {
		if got := DetermineRole(c.title, c.typ, c.email); got != c.want {
			t.Errorf("DetermineRole(%q,%q,%q)=%q, chci %q", c.title, c.typ, c.email, got, c.want)
		}
	}
}
