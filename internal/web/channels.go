package web

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"sbibolet/internal/config"
)

const (
	permViewChannel = 1 << 10
	channelsTTL     = 15 * time.Minute
	channelsShown   = 4
)

// fallbackChannels se zobrazi, kdyz neni k dispozici Discord (lokalni vyvoj, vypadek API)
var fallbackChannels = []string{"akce-na-fm-a-tul", "do-zahraničí", "stáže-praxe-práce", "soutěže-a-příležitosti"}

type permOverwrite struct {
	ID    string `json:"id"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type guildChannel struct {
	ID         string          `json:"id"`
	Type       int             `json:"type"`
	Name       string          `json:"name"`
	ParentID   string          `json:"parent_id"`
	Overwrites []permOverwrite `json:"permission_overwrites"`
}

// everyoneView vraci 1 (@everyone smi kanal videt), -1 (nesmi) nebo 0 (bez prepisu)
func everyoneView(ow []permOverwrite, guildID string) int {
	for _, o := range ow {
		if o.ID != guildID {
			continue
		}
		allow, _ := strconv.ParseUint(o.Allow, 10, 64)
		deny, _ := strconv.ParseUint(o.Deny, 10, 64)
		if allow&permViewChannel != 0 {
			return 1
		}
		if deny&permViewChannel != 0 {
			return -1
		}
	}
	return 0
}

// lockedChannelNames vybere textove kanaly, ktere neovereny uzivatel (@everyone) nevidi.
// Kanal bez vlastniho prepisu dedi pravo od kategorie.
func lockedChannelNames(chs []guildChannel, guildID string) []string {
	cats := map[string]int{}
	for _, c := range chs {
		if c.Type == 4 {
			cats[c.ID] = everyoneView(c.Overwrites, guildID)
		}
	}
	var names []string
	for _, c := range chs {
		if c.Type != 0 && c.Type != 5 {
			continue
		}
		v := everyoneView(c.Overwrites, guildID)
		if v == 0 && c.ParentID != "" {
			v = cats[c.ParentID]
		}
		if v < 0 {
			names = append(names, c.Name)
		}
	}
	return names
}

var chanCache struct {
	sync.Mutex
	names []string
	at    time.Time
	busy  bool
}

// refreshChannels stahne kanaly serveru z Discordu a ulozi neverejne do cache
func refreshChannels() {
	defer func() {
		chanCache.Lock()
		chanCache.busy = false
		chanCache.Unlock()
	}()
	guildID, token := config.Cfg.DiscordGuildID, config.Cfg.DiscordToken
	if guildID == "" || token == "" || token == "SEM_VLOZTE_DISCORD_BOT_TOKEN" {
		return
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://discord.com/api/v10/guilds/%s/channels", guildID), nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bot "+token)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("Kanály z Discordu se nepodařilo stáhnout: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Kanály z Discordu: HTTP %d", resp.StatusCode)
		return
	}
	var chs []guildChannel
	if err := json.NewDecoder(resp.Body).Decode(&chs); err != nil {
		return
	}
	names := lockedChannelNames(chs, guildID)
	chanCache.Lock()
	chanCache.names, chanCache.at = names, time.Now()
	chanCache.Unlock()
}

// randomChannels vrati n nahodnych neverejnych kanalu (nebo zalozni seznam)
func randomChannels(n int) []string {
	chanCache.Lock()
	pool := append([]string(nil), chanCache.names...)
	if time.Since(chanCache.at) > channelsTTL && !chanCache.busy {
		chanCache.busy = true
		go refreshChannels()
	}
	chanCache.Unlock()
	if len(pool) == 0 {
		pool = append(pool, fallbackChannels...)
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// termLine je jeden radek terminalu s casovanim psani
type termLine struct {
	Name  string
	N     int
	Delay string // zacatek psani
	End   string // konec psani
}

// terminalLines spocita casovani, aby se radky psaly jeden po druhem
func terminalLines(names []string) (termLine, []termLine) {
	t := 0.5
	cmdText := "ls kanaly/"
	cmd := termLine{Name: cmdText, N: utf8.RuneCountInString(cmdText), Delay: fmt.Sprintf("%.2fs", t)}
	t += 0.06 * float64(cmd.N)
	cmd.End = fmt.Sprintf("%.2fs", t)
	t += 0.25
	lines := make([]termLine, 0, len(names))
	for _, n := range names {
		l := termLine{Name: n, N: cellWidth(n) + 1, Delay: fmt.Sprintf("%.2fs", t)}
		t += 0.06 * float64(l.N)
		l.End = fmt.Sprintf("%.2fs", t)
		t += 0.15
		lines = append(lines, l)
	}
	return cmd, lines
}

// cellWidth odhadne sirku textu v nasobcich sirky jednoho znaku neproporcionalniho pisma:
// emoji a symboly jsou siroke 2, nulove spojovace 0, ostatni 1 (aby se nazvy v terminalu neorezavaly)
func cellWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == 0xFE0F || r == 0x200D:
			// variation selector a spojovac nemaji sirku
		case r >= 0x1F000, r >= 0x2600 && r <= 0x27BF, r >= 0x2B00 && r <= 0x2BFF, r >= 0x2300 && r <= 0x23FF:
			w += 2
		default:
			w++
		}
	}
	return w
}
