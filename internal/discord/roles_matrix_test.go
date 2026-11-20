package discord

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"sbibolet/internal/config"
	"sbibolet/internal/storage"

	"github.com/bwmarrin/discordgo"
)

type fakeDiscord struct {
	mu        sync.Mutex
	member    bool
	joinRoles []string
	added     []string
	removed   []string
	failRole  string
	joinCode  int
}

func (f *fakeDiscord) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/roles") && r.Method == "GET":
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "1", "name": "Student FM"}, {"id": "2", "name": "Zaměstnanec FM"},
			{"id": "3", "name": "ME"}, {"id": "4", "name": "IT"}, {"id": "5", "name": "AVI"},
			{"id": "6", "name": "Absolvent FM"}, {"id": "7", "name": "Ověřený"},
		})
	case strings.Contains(p, "/members/") && strings.Contains(p, "/roles/"):
		id := p[strings.LastIndex(p, "/")+1:]
		if r.Method == "PUT" {
			if id == f.failRole {
				w.WriteHeader(403)
				return
			}
			f.added = append(f.added, id)
		} else {
			f.removed = append(f.removed, id)
		}
		w.WriteHeader(204)
	case strings.Contains(p, "/members/") && r.Method == "GET":
		if !f.member {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	case strings.Contains(p, "/members/") && r.Method == "PUT":
		var body struct{ Roles []string }
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		f.joinRoles = body.Roles
		if f.joinCode != 0 {
			w.WriteHeader(f.joinCode)
			return
		}
		w.WriteHeader(201)
	default:
		w.WriteHeader(204)
	}
}

type rewrite struct{ target *url.URL }

var baseTransport = http.DefaultTransport

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme, r.URL.Host = rw.target.Scheme, rw.target.Host
	return baseTransport.RoundTrip(r)
}

func TestJoinAndRoleMatrix(t *testing.T) {
	fd := &fakeDiscord{}
	srv := httptest.NewServer(fd)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	old := http.DefaultTransport
	http.DefaultTransport = rewrite{u}
	defer func() { http.DefaultTransport = old }()
	oldSess := Session
	Session, _ = discordgo.New("Bot x")
	Session.Client = &http.Client{Transport: rewrite{u}}
	defer func() { Session = oldSess }()
	config.Cfg.DiscordGuildID = "123456789012345678"
	config.Cfg.DiscordToken = "x"

	type tc struct {
		name    string
		st      storage.Student
		member  bool
		want    []string // role ID (sorted); pro clena prirazene PUT, pro nove pres join
		removed []string
	}
	cases := []tc{}
	for _, member := range []bool{false, true} {
		for _, p := range []string{"ME", "IT", "AVI", "", "-", "XX"} {
			want := []string{"1"}
			var rem []string
			switch p {
			case "ME":
				want = []string{"1", "3"}
				rem = []string{"4", "5"}
			case "IT":
				want = []string{"1", "4"}
				rem = []string{"3", "5"}
			case "AVI":
				want = []string{"1", "5"}
				rem = []string{"3", "4"}
			}
			if !member {
				rem = nil
			}
			cases = append(cases, tc{"student/" + p, storage.Student{Name: "A B", Role: "Student FM", Program: p}, member, want, rem})
		}
		cases = append(cases,
			tc{"zamestnanec", storage.Student{Name: "A B", Role: "Zaměstnanec FM", Program: "-"}, member, []string{"2"}, nil},
			tc{"zamestnanec+student", storage.Student{Name: "A B", Role: "Zaměstnanec FM", AlsoStudent: true, Program: "-"}, member, []string{"1", "2"}, nil},
			tc{"absolvent", storage.Student{Name: "x", Role: "Absolvent FM", Program: "-"}, member, []string{"6"}, nil},
		)
	}
	for _, c := range cases {
		*fd = fakeDiscord{member: c.member}
		st := c.st
		if err := PerformJoinAndRole("987654321098765432", "tok", &st); err != nil {
			t.Errorf("%s (member=%v): neocekavana chyba %v", c.name, c.member, err)
			continue
		}
		got := fd.joinRoles
		if c.member {
			got = fd.added
		}
		got = append([]string(nil), got...)
		sort.Strings(got)
		rem := append([]string(nil), fd.removed...)
		sort.Strings(rem)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s (member=%v): role %v, ocekavano %v", c.name, c.member, got, c.want)
		}
		if strings.Join(rem, ",") != strings.Join(c.removed, ",") {
			t.Errorf("%s (member=%v): odebrane %v, ocekavano %v", c.name, c.member, rem, c.removed)
		}
	}

	// chyby Discordu se nesmi tvarit jako uspech
	*fd = fakeDiscord{member: true, failRole: "1"}
	st := storage.Student{Name: "A", Role: "Student FM", Program: "ME"}
	if err := PerformJoinAndRole("987654321098765432", "tok", &st); err == nil {
		t.Error("clen: selhani PUT role Student FM (403) vratilo uspech")
	}
	*fd = fakeDiscord{member: false, joinCode: 403}
	if err := PerformJoinAndRole("987654321098765432", "tok", &st); err == nil {
		t.Error("novy clen: odmitnuty guilds.join vratil uspech")
	}
}
