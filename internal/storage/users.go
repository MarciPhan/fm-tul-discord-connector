package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sbibolet/internal/security"
)

// Student reprezentuje overeneho studenta ci zamestnance
type Student struct {
	MicrosoftID string    `json:"microsoft_id"`
	DiscordID   string    `json:"discord_id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Faculty     string    `json:"faculty"`
	Role        string    `json:"role"` // "Student FM" nebo "Zaměstnanec FM"
	VerifiedAt  time.Time `json:"verified_at"`
}

// UsersStorageFile je cesta k souboru s databazi uzivatelu
var UsersStorageFile = "users.json"

var (
	usersDB  = make(map[string]*Student) // indexovano podle DiscordID
	usersMux sync.RWMutex
)

// CheckBindingAllowed zajistuje striktni vazbu 1:1 mezi TUL uctem a Discord uctem
func CheckBindingAllowed(s *Student) error {
	usersMux.RLock()
	defer usersMux.RUnlock()

	for _, u := range usersDB {
		// Stejne Microsoft ID nesmi overit jiny Discord ucet
		if u.MicrosoftID == s.MicrosoftID && u.DiscordID != s.DiscordID {
			return fmt.Errorf("tento univerzitní TUL účet (%s) je již spárován s jiným Discord účtem", s.Email)
		}
		// Stejny Discord ucet nesmi ziskat jine Microsoft ID
		if u.DiscordID == s.DiscordID && u.MicrosoftID != s.MicrosoftID {
			return fmt.Errorf("tento Discord účet je již spárován s jiným univerzitním účtem")
		}
	}
	return nil
}

// LoadUsers nacte stavajici uzivatele z disku
func LoadUsers() {
	usersMux.Lock()
	defer usersMux.Unlock()

	data, err := os.ReadFile(UsersStorageFile)
	if err != nil {
		return // Soubor zatim neexistuje
	}
	var list []*Student
	if err := json.Unmarshal(data, &list); err == nil {
		for _, u := range list {
			usersDB[u.DiscordID] = u
		}
		log.Printf("📂 Načteno %d ověřených uživatelů z %s", len(list), UsersStorageFile)
	}
}

// SaveUser provede bezpecny atomicky zapis s pravy 0600
func SaveUser(s *Student) error {
	if err := CheckBindingAllowed(s); err != nil {
		return err
	}

	usersMux.Lock()
	defer usersMux.Unlock()

	usersDB[s.DiscordID] = s

	list := make([]*Student, 0, len(usersDB))
	for _, u := range usersDB {
		list = append(list, u)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	// Atomicky zapis: zapis do docasneho souboru a nasledny rename
	dir := filepath.Dir(UsersStorageFile)
	if dir == "" || dir == "." {
		dir = "."
	}
	tempFile, err := os.CreateTemp(dir, "users_*.tmp")
	if err != nil {
		return fmt.Errorf("chyba vytvoreni docasneho souboru: %w", err)
	}
	tempName := tempFile.Name()

	// Zabezpeceni prav: pouze vlastnik procesu muze cist a zapisovat
	_ = tempFile.Chmod(0600)

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}

	if err := os.Rename(tempName, UsersStorageFile); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

// GetUserByDiscordID vrati uzivatele podle Discord ID
func GetUserByDiscordID(discordID string) (*Student, bool) {
	usersMux.RLock()
	defer usersMux.RUnlock()
	u, ok := usersDB[discordID]
	return u, ok
}

// GetUserCount vrati pocet overenych uzivatelu
func GetUserCount() int {
	usersMux.RLock()
	defer usersMux.RUnlock()
	return len(usersDB)
}

// ResetDB vycisti databazi (pouzivano v testech)
func ResetDB() {
	usersMux.Lock()
	defer usersMux.Unlock()
	usersDB = make(map[string]*Student)
}

// IsAlreadyVerified zjisti, zda je uzivatel jiz overeny
func IsAlreadyVerified(discordID string) bool {
	usersMux.RLock()
	defer usersMux.RUnlock()
	_, ok := usersDB[discordID]
	return ok
}

// init – placeholder, tato funkce je volana rucne (LoadUsers)
var _ = security.GenerateID // zajisteni importu
