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

// clubMessageTitle je zacatek titulku zpravy s prihlasenim do studentskych spolku
const clubMessageTitle = "Přihlas se do studentských spolků"

// reactionRoleSet popisuje jednu zpravu, jejiz reakce prideluji role
type reactionRoleSet struct {
	title     string            // text, ktery musi obsahovat titulek embedu zpravy
	roles     map[string]string // emoji (bez variation selectoru) -> nazev role na serveru
	exclusive []string          // emoji, ze kterych ma clovek jen jedno
}

var (
	programSet = reactionRoleSet{
		title: programMessageTitle,
		roles: map[string]string{
			"🔧": "ME",
			"💻": "IT",
			"🌐": "AVI",
			"🎖": "Absolvent FM",
		},
		exclusive: []string{"🔧", "💻", "🌐"},
	}

	clubSet = reactionRoleSet{
		title: clubMessageTitle,
		roles: map[string]string{
			"🏎":  "Formule",
			"🤝":  "SBC",
			"🌍":  "ESN",
			"🔬":  "TULab",
			"🇪🇺": "RUN-EU Studenti",
		},
	}

	reactionRoleSets = []*reactionRoleSet{&programSet, &clubSet}
)

// verifiedRoleNames jsou nazvy rolí, podle kterych poznáme overeneho clena, kdyz neni nastavene ID
var verifiedRoleNames = []string{"Student FM", "Zaměstnanec FM", "Ověřený"}

var (
	reactionMessageSets    = make(map[string]*reactionRoleSet)
	reactionMessageSetsMux sync.Mutex
)

// normalizeEmoji odstrani variation selector, aby 🎖 a 🎖️ byly totez
func normalizeEmoji(name string) string {
	return strings.ReplaceAll(name, "️", "")
}

// programRoleName vrati nazev role pro programove emoji, nebo "" pokud emoji neni programove
func programRoleName(emoji string) string {
	return programSet.roles[normalizeEmoji(emoji)]
}

// isExclusive rika, zda je emoji jednim z vzajemne se vylucujicich programu
func isExclusive(emoji string) bool {
	return programSet.isExclusive(emoji)
}

func (rs *reactionRoleSet) isExclusive(emoji string) bool {
	e := normalizeEmoji(emoji)
	for _, x := range rs.exclusive {
		if x == e {
			return true
		}
	}
	return false
}

// findReactionSet zjisti, zda je zprava jednou ze zprav s reakcnimi rolemi od tohoto bota
func findReactionSet(s *discordgo.Session, channelID, messageID string) *reactionRoleSet {
	reactionMessageSetsMux.Lock()
	known, ok := reactionMessageSets[messageID]
	reactionMessageSetsMux.Unlock()
	if ok {
		return known
	}

	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil || msg == nil || msg.Author == nil || s.State == nil || s.State.User == nil {
		return nil
	}
	if msg.Author.ID != s.State.User.ID {
		return nil
	}
	for _, e := range msg.Embeds {
		for _, rs := range reactionRoleSets {
			if strings.Contains(e.Title, rs.title) {
				reactionMessageSetsMux.Lock()
				reactionMessageSets[messageID] = rs
				reactionMessageSetsMux.Unlock()
				return rs
			}
		}
	}
	return nil
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

// HandleProgramReactionAdd prideli roli podle reakce (pouze overenym clenum)
func HandleProgramReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r == nil || r.GuildID == "" || s.State == nil || s.State.User == nil || r.UserID == s.State.User.ID {
		return
	}
	emoji := normalizeEmoji(r.Emoji.Name)
	if !isAnyReactionEmoji(emoji) {
		return
	}
	set := findReactionSet(s, r.ChannelID, r.MessageID)
	if set == nil {
		return
	}
	roleName, ok := set.roles[emoji]
	if !ok {
		return
	}

	member, err := s.GuildMember(r.GuildID, r.UserID)
	if err != nil {
		log.Printf("Nelze načíst člena %s pro reakční roli: %v", r.UserID, err)
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
		log.Printf("Role %q na serveru neexistuje, reakce přeskočena", roleName)
		return
	}

	// vylucne emoji: odebereme ostatni role z teto skupiny i jejich reakce
	if set.isExclusive(emoji) {
		for _, other := range set.exclusive {
			if other == emoji {
				continue
			}
			if otherID, ok := byName[set.roles[other]]; ok {
				_ = s.GuildMemberRoleRemove(r.GuildID, r.UserID, otherID)
			}
			_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, other, r.UserID)
		}
	}

	if err := s.GuildMemberRoleAdd(r.GuildID, r.UserID, roleID); err != nil {
		log.Printf("Nelze přidat roli %s uživateli %s: %v", roleName, r.UserID, err)
		return
	}
	log.Printf("Reakční role: uživatel %s dostal roli %s", r.UserID, roleName)
}

// HandleProgramReactionRemove odebere roli, kdyz clovek sundá reakci
func HandleProgramReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if r == nil || r.GuildID == "" || s.State == nil || s.State.User == nil || r.UserID == s.State.User.ID {
		return
	}
	emoji := normalizeEmoji(r.Emoji.Name)
	if !isAnyReactionEmoji(emoji) {
		return
	}
	set := findReactionSet(s, r.ChannelID, r.MessageID)
	if set == nil {
		return
	}
	roleName, ok := set.roles[emoji]
	if !ok {
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
	log.Printf("Reakční role: uživateli %s odebrána role %s", r.UserID, roleName)
}

// isAnyReactionEmoji je rychly predfiltr, aby se zbytecne nevolalo API u cizich reakci
func isAnyReactionEmoji(emoji string) bool {
	for _, rs := range reactionRoleSets {
		if _, ok := rs.roles[emoji]; ok {
			return true
		}
	}
	return false
}
