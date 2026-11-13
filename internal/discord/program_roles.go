package discord

import (
	"log"
	"strings"
	"sync"

	"sbibolet/internal/config"

	"github.com/bwmarrin/discordgo"
)

// programMessageTitle je zacatek titulku zpravy s vyberem studijniho programu
const programMessageTitle = "Vyber si svůj studijní program"

// programRoles mapuje emoji na nazev role na serveru
var programRoles = map[string]string{
	"🔧": "ME",
	"💻": "IT",
	"🌐": "AVI",
	"🎖": "Absolvent FM",
}

// exclusiveProgramEmojis jsou programy, z nichz ma clovek jen jeden (absolvent je navic)
var exclusiveProgramEmojis = []string{"🔧", "💻", "🌐"}

// verifiedRoleNames jsou nazvy rolí, podle kterych poznáme overeneho clena, kdyz neni nastavene ID
var verifiedRoleNames = []string{"Student FM", "Zaměstnanec FM", "Ověřený"}

var (
	programMessageIDs    = make(map[string]bool)
	programMessageIDsMux sync.Mutex
)

// normalizeEmoji odstrani variation selector, aby 🎖 a 🎖️ byly totez
func normalizeEmoji(name string) string {
	return strings.ReplaceAll(name, "️", "")
}

// programRoleName vrati nazev role pro emoji, nebo "" pokud emoji neni programove
func programRoleName(emoji string) string {
	return programRoles[normalizeEmoji(emoji)]
}

// isProgramMessage zjisti, zda je zprava vyber studijniho programu od tohoto bota
func isProgramMessage(s *discordgo.Session, channelID, messageID string) bool {
	programMessageIDsMux.Lock()
	known := programMessageIDs[messageID]
	programMessageIDsMux.Unlock()
	if known {
		return true
	}

	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil || msg == nil || msg.Author == nil || s.State == nil || s.State.User == nil {
		return false
	}
	if msg.Author.ID != s.State.User.ID {
		return false
	}
	for _, e := range msg.Embeds {
		if strings.Contains(e.Title, programMessageTitle) {
			programMessageIDsMux.Lock()
			programMessageIDs[messageID] = true
			programMessageIDsMux.Unlock()
			return true
		}
	}
	return false
}

// guildRoleIDsByName vrati mapu nazev -> ID rolí serveru
func guildRoleIDsByName(s *discordgo.Session, guildID string) map[string]string {
	roles, err := s.GuildRoles(guildID)
	if err != nil {
		log.Printf("Nelze načíst role serveru: %v", err)
		return nil
	}
	byName := make(map[string]string, len(roles))
	for _, r := range roles {
		byName[r.Name] = r.ID
	}
	return byName
}

// isVerifiedMember rozhoduje, zda ma clen roli overeneho studenta/zamestnance
func isVerifiedMember(member *discordgo.Member, byName map[string]string) bool {
	allowed := map[string]bool{}
	for _, id := range []string{config.GetVerifiedRoleID(), config.GetFMStudentRoleID(), config.GetFMStaffRoleID()} {
		if id != "" {
			allowed[id] = true
		}
	}
	for _, n := range verifiedRoleNames {
		if id, ok := byName[n]; ok {
			allowed[id] = true
		}
	}
	for _, r := range member.Roles {
		if allowed[r] {
			return true
		}
	}
	return false
}

// HandleProgramReactionAdd prideli roli podle programove reakce (pouze overenym clenum)
func HandleProgramReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r == nil || r.GuildID == "" || s.State == nil || s.State.User == nil || r.UserID == s.State.User.ID {
		return
	}
	roleName := programRoleName(r.Emoji.Name)
	if roleName == "" || !isProgramMessage(s, r.ChannelID, r.MessageID) {
		return
	}

	member, err := s.GuildMember(r.GuildID, r.UserID)
	if err != nil {
		log.Printf("Nelze načíst člena %s pro výběr programu: %v", r.UserID, err)
		return
	}
	byName := guildRoleIDsByName(s, r.GuildID)
	if byName == nil {
		return
	}

	if !isVerifiedMember(member, byName) {
		_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.APIName(), r.UserID)
		return
	}

	roleID, ok := byName[roleName]
	if !ok {
		log.Printf("Role %q na serveru neexistuje, výběr programu přeskočen", roleName)
		return
	}

	// programy se vylucuji: odebereme ostatni programove role i jejich reakce
	if isExclusive(r.Emoji.Name) {
		for _, other := range exclusiveProgramEmojis {
			if other == normalizeEmoji(r.Emoji.Name) {
				continue
			}
			if otherID, ok := byName[programRoles[other]]; ok {
				_ = s.GuildMemberRoleRemove(r.GuildID, r.UserID, otherID)
			}
			_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, other, r.UserID)
		}
	}

	if err := s.GuildMemberRoleAdd(r.GuildID, r.UserID, roleID); err != nil {
		log.Printf("Nelze přidat roli %s uživateli %s: %v", roleName, r.UserID, err)
		return
	}
	log.Printf("Výběr programu: uživatel %s dostal roli %s", r.UserID, roleName)
}

// HandleProgramReactionRemove odebere roli, kdyz clovek sundá programovou reakci
func HandleProgramReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if r == nil || r.GuildID == "" || s.State == nil || s.State.User == nil || r.UserID == s.State.User.ID {
		return
	}
	roleName := programRoleName(r.Emoji.Name)
	if roleName == "" || !isProgramMessage(s, r.ChannelID, r.MessageID) {
		return
	}
	byName := guildRoleIDsByName(s, r.GuildID)
	roleID, ok := byName[roleName]
	if !ok {
		return
	}
	if err := s.GuildMemberRoleRemove(r.GuildID, r.UserID, roleID); err != nil {
		log.Printf("Nelze odebrat roli %s uživateli %s: %v", roleName, r.UserID, err)
		return
	}
	log.Printf("Výběr programu: uživateli %s odebrána role %s", r.UserID, roleName)
}

func isExclusive(emoji string) bool {
	e := normalizeEmoji(emoji)
	for _, x := range exclusiveProgramEmojis {
		if x == e {
			return true
		}
	}
	return false
}
