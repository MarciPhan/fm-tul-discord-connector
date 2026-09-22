package discord

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"sbibolet/internal/audit"
	"sbibolet/internal/config"
	"sbibolet/internal/security"

	"github.com/bwmarrin/discordgo"
	"github.com/mmcdole/gofeed"
)

// FeedEntry ukládá informace o odebíraném RSS zdroji
type FeedEntry struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ChannelID string    `json:"channel_id"`
	AddedBy   string    `json:"added_by"`
	LastGuid  string    `json:"last_guid"` // ID naposledy odeslaného článku
	UpdatedAt time.Time `json:"updated_at"`
}

var (
	feedsDB      = make(map[string]*FeedEntry)
	feedsMux     sync.RWMutex
	FeedsFile    = "feeds.json"
	rssTicker    *time.Ticker
	stopRSSChan  chan struct{}
	stopRSSOnce  sync.Once
)

// isValidRSSURL ověřuje formát URL a chrání proti SSRF útokům na interní sítě
func isValidRSSURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("neplatný formát URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("povoleny jsou pouze http a https protokoly")
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" || hostname == "localhost" || hostname == "::1" || strings.HasPrefix(hostname, "127.") {
		return fmt.Errorf("nelze přidat lokální síťovou adresu (SSRF ochrana)")
	}
	// Explicitní blokace cloud metadata endpoint (AWS, GCP, Azure IMDS)
	if hostname == "169.254.169.254" || hostname == "metadata.google.internal" {
		return fmt.Errorf("nelze přidat cloud metadata adresu (SSRF ochrana)")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("nelze přidat privátní nebo interní IP adresu (SSRF ochrana)")
		}
	}
	return nil
}

// loadFeeds načte uložené feedy ze souboru
func loadFeeds() {
	feedsMux.Lock()
	defer feedsMux.Unlock()
	data, err := os.ReadFile(FeedsFile)
	if err != nil {
		return
	}
	var list []*FeedEntry
	if err := json.Unmarshal(data, &list); err == nil {
		for _, f := range list {
			feedsDB[f.ID] = f
		}
		log.Printf("Načteno %d RSS feedů.", len(list))
	}
}

// saveFeeds uloží feedy do souboru atomicky
func saveFeeds() {
	feedsMux.RLock()
	var list []*FeedEntry
	for _, f := range feedsDB {
		list = append(list, f)
	}
	feedsMux.RUnlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("RSS save error: %v", err)
		return
	}
	tmpFile, err := os.CreateTemp(".", "feeds_*.tmp")
	if err != nil {
		return
	}
	tmpName := tmpFile.Name()
	_ = tmpFile.Chmod(0600)
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, FeedsFile)
}

// StartRSSWorker spustí periodické kontroly RSS
func StartRSSWorker(s *discordgo.Session) {
	if !config.Cfg.RSSEnabled {
		return
	}
	loadFeeds()

	rssTicker = time.NewTicker(config.Cfg.RSSCheckInterval)
	stopRSSChan = make(chan struct{})

	go func() {
		log.Println("RSS Worker spuštěn.")
		// Okamžitá první kontrola (volitelně, raději počkáme na první tik nebo uděláme hned)
		checkAllFeeds(s)

		for {
			select {
			case <-rssTicker.C:
				checkAllFeeds(s)
			case <-stopRSSChan:
				log.Println("RSS Worker zastaven.")
				return
			}
		}
	}()
}

func StopRSSWorker() {
	if rssTicker != nil {
		rssTicker.Stop()
	}
	if stopRSSChan != nil {
		stopRSSOnce.Do(func() { close(stopRSSChan) })
	}
}

func checkAllFeeds(s *discordgo.Session) {
	feedsMux.RLock()
	entries := make([]*FeedEntry, 0, len(feedsDB))
	for _, f := range feedsDB {
		entries = append(entries, &FeedEntry{
			ID:        f.ID,
			URL:       f.URL,
			ChannelID: f.ChannelID,
			AddedBy:   f.AddedBy,
			LastGuid:  f.LastGuid,
			UpdatedAt: f.UpdatedAt,
		})
	}
	feedsMux.RUnlock()

	client := &http.Client{Timeout: 10 * time.Second}
	fp := gofeed.NewParser()
	fp.Client = client

	updates := make(map[string]string) // ID -> newGuid

	for _, feedEntry := range entries {
		feed, err := fp.ParseURL(feedEntry.URL)
		if err != nil {
			log.Printf("RSS chyba načítání %s: %v", feedEntry.URL, err)
			continue
		}

		if len(feed.Items) == 0 {
			continue
		}

		// Předpokládáme, že první je nejnovější
		latestItem := feed.Items[0]

		// Unikátní identifikátor článku (preferuje GUID, pak link, pak titulek)
		guid := latestItem.GUID
		if guid == "" {
			guid = latestItem.Link
		}
		if guid == "" {
			guid = latestItem.Title
		}

		// Pokud se GUID liší od posledně uloženého, máme nový článek
		if feedEntry.LastGuid != guid {
			if feedEntry.LastGuid != "" { // Neodesílat při prvním načtení feedu celou historii, jen si uložit první GUID
				sendRSSUpdate(s, feedEntry.ChannelID, feed.Title, latestItem)
			} else {
				log.Printf("RSS Inicializováno: %s", feedEntry.URL)
			}
			updates[feedEntry.ID] = guid
		}
	}

	if len(updates) > 0 {
		feedsMux.Lock()
		for id, newGuid := range updates {
			if entry, ok := feedsDB[id]; ok {
				entry.LastGuid = newGuid
				entry.UpdatedAt = time.Now()
			}
		}
		feedsMux.Unlock()
		saveFeeds()
	}
}

func sendRSSUpdate(s *discordgo.Session, channelID, sourceName string, item *gofeed.Item) {
	desc := item.Description
	if len(desc) > 300 {
		desc = desc[:297] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       item.Title,
		URL:         item.Link,
		Description: desc,
		Color:       0x0284c7, // TUL modrá
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Zdroj: %s", sourceName),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if item.PublishedParsed != nil {
		embed.Timestamp = item.PublishedParsed.Format(time.RFC3339)
	}

	_, err := s.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("RSS Send error to %s: %v", channelID, err)
	} else {
		log.Printf("RSS odesláno: %s do %s", item.Title, channelID)
	}
}

// --- SLASH COMMAND ---

type CmdRSS struct{}

func (c *CmdRSS) Info() *discordgo.ApplicationCommand {
	adminPerm := int64(discordgo.PermissionManageMessages)
	return &discordgo.ApplicationCommand{
		Name:                     "rss",
		Description:              "Správa RSS feedů (TUL a FM TUL)",
		DefaultMemberPermissions: &adminPerm,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "add",
				Description: "Přidá nový RSS feed",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "url",
						Description: "URL adresa RSS feedu (např. https://tuni.tul.cz/feed/)",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "kanal",
						Description: "Kanál, kam posílat novinky",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "list",
				Description: "Zobrazí sledované RSS feedy",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "remove",
				Description: "Odstraní RSS feed",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "id",
						Description: "ID feedu (zjistíš pomocí /rss list)",
						Required:    true,
					},
				},
			},
		},
	}
}

func (c *CmdRSS) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !config.Cfg.RSSEnabled {
		respondEphemeral(s, i, "RSS modul není povolen. Změň RSS_ENABLED v .env.")
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	username := GetInteractionUsername(i)
	userID := GetInteractionUserID(i)

	subcmd := options[0]
	switch subcmd.Name {
	case "add":
		var url, channelID string
		for _, opt := range subcmd.Options {
			if opt.Name == "url" {
				url = strings.TrimSpace(opt.StringValue())
			} else if opt.Name == "kanal" {
				if ch := opt.ChannelValue(s); ch != nil {
					channelID = ch.ID
				}
			}
		}

		if err := isValidRSSURL(url); err != nil {
			respondEphemeral(s, i, fmt.Sprintf("Neplatná nebo nepovolená URL feedu: %v", err))
			return
		}

		// Zkusebni parsovani s timeoutem
		client := &http.Client{Timeout: 10 * time.Second}
		fp := gofeed.NewParser()
		fp.Client = client
		_, err := fp.ParseURL(url)
		if err != nil {
			respondEphemeral(s, i, fmt.Sprintf("Chyba načtení RSS feedu (je adresa správná?): %v", err))
			return
		}

		id := fmt.Sprintf("rss_%s", security.GenerateID()[:12])
		feedsMux.Lock()
		feedsDB[id] = &FeedEntry{
			ID:        id,
			URL:       url,
			ChannelID: channelID,
			AddedBy:   userID,
			UpdatedAt: time.Now(),
		}
		feedsMux.Unlock()
		saveFeeds()
		respondEphemeral(s, i, fmt.Sprintf("RSS feed `%s` úspěšně přidán pro kanál <#%s>. (ID: `%s`)", url, channelID, id))
		audit.Log(audit.LevelInfo, "RSS přidáno", fmt.Sprintf("Správce **%s** přidal RSS feed `%s` pro kanál <#%s>.", username, url, channelID))

	case "list":
		feedsMux.RLock()
		defer feedsMux.RUnlock()
		if len(feedsDB) == 0 {
			respondEphemeral(s, i, "Nejsou sledovány žádné RSS feedy.")
			return
		}
		msg := "**Sledované RSS feedy:**\n"
		for id, f := range feedsDB {
			msg += fmt.Sprintf("• ID: `%s` | <#%s> | %s\n", id, f.ChannelID, f.URL)
		}
		respondEphemeral(s, i, msg)

	case "remove":
		id := subcmd.Options[0].StringValue()
		feedsMux.Lock()
		if _, ok := feedsDB[id]; ok {
			delete(feedsDB, id)
			feedsMux.Unlock()
			saveFeeds()
			respondEphemeral(s, i, fmt.Sprintf("RSS feed `%s` byl odstraněn.", id))
			audit.Log(audit.LevelWarning, "RSS odebráno", fmt.Sprintf("Správce **%s** odebral RSS feed s ID `%s`.", username, id))
		} else {
			feedsMux.Unlock()
			respondEphemeral(s, i, "Feed s tímto ID nebyl nalezen.")
		}
	}
}

func init() {
	CmdRegistry.Register(&CmdRSS{})
}
