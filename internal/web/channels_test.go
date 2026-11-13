package web

import (
	"sort"
	"testing"
)

func TestLockedChannelNames(t *testing.T) {
	const guild = "100"
	deny := "1024"
	chs := []guildChannel{
		{ID: "1", Type: 4, Name: "START"},
		{ID: "2", Type: 4, Name: "FM", Overwrites: []permOverwrite{{ID: guild, Deny: deny}}},
		{ID: "3", Type: 0, Name: "pravidla", ParentID: "1"},
		{ID: "4", Type: 0, Name: "akce", ParentID: "2"},
		{ID: "5", Type: 0, Name: "verejny-v-zamcene-kategorii", ParentID: "2", Overwrites: []permOverwrite{{ID: guild, Allow: deny}}},
		{ID: "6", Type: 0, Name: "vlastni-zamek", Overwrites: []permOverwrite{{ID: guild, Deny: deny}}},
		{ID: "7", Type: 2, Name: "hlasovy", ParentID: "2"},
	}
	got := lockedChannelNames(chs, guild)
	sort.Strings(got)
	want := []string{"akce", "vlastni-zamek"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lockedChannelNames=%v, chci %v", got, want)
	}
}

func TestRandomChannelsFallback(t *testing.T) {
	chanCache.Lock()
	chanCache.names = nil
	chanCache.busy = true // zabranit stahovani v testu
	chanCache.Unlock()
	if got := randomChannels(4); len(got) != 4 {
		t.Fatalf("chci 4 kanaly, mam %v", got)
	}
	if got := randomChannels(2); len(got) != 2 {
		t.Fatalf("chci 2 kanaly, mam %v", got)
	}
}

func TestTerminalLines(t *testing.T) {
	cmd, lines := terminalLines([]string{"abc", "de"})
	if cmd.N == 0 || len(lines) != 2 || lines[0].N != 4 { // sirka + 1 znak rezerva
		t.Fatalf("spatne casovani: %+v %+v", cmd, lines)
	}
}

func TestCellWidth(t *testing.T) {
	cases := map[string]int{
		"tipy-na-akce":           12,
		"📣-oznámení-discord":     18 + 1, // emoji = 2, zbytek 17
		"🇪🇺-run-eu-studenti":     20,
		"✅-přihlas-se-do-spolku": 24,
	}
	for in, want := range cases {
		if got := cellWidth(in); got < want-1 {
			t.Errorf("cellWidth(%q)=%d, očekáváno aspoň %d", in, got, want-1)
		}
	}
	if cellWidth("a\uFE0Fb") != 2 {
		t.Error("variation selector nemá mít šířku")
	}
}
