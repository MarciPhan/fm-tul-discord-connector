package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// DynamicConfig reprezentuje nastavení, která se dají měnit za běhu z Discordu
type DynamicConfig struct {
	DiscordWelcomeChannelID string `json:"welcome_channel_id,omitempty"`
	DiscordAuditChannelID   string `json:"audit_channel_id,omitempty"`

	DiscordVerifiedRoleID string `json:"verified_role_id,omitempty"`
	DiscordFMStudentID    string `json:"fm_student_role_id,omitempty"`
	DiscordFMStaffID      string `json:"fm_staff_role_id,omitempty"`

	WelcomeMessage string `json:"welcome_message,omitempty"`
}

var (
	DynCfg     DynamicConfig
	dynCfgMux  sync.RWMutex
	ConfigFile = "discord_config.json"
)

func init() {
	LoadDynamic()
}

// LoadDynamic načte dynamickou konfiguraci ze souboru (pokud existuje)
func LoadDynamic() {
	dynCfgMux.Lock()
	defer dynCfgMux.Unlock()

	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Nelze načíst %s: %v", ConfigFile, err)
		}
		return // Soubor neexistuje, nevadi
	}

	if err := json.Unmarshal(data, &DynCfg); err != nil {
		log.Printf("Chyba parsování %s: %v", ConfigFile, err)
	}
}

// SaveDynamic uloží aktuální stav dynamické konfigurace na disk (atomicky)
func SaveDynamic() error {
	dynCfgMux.RLock()
	data, err := json.MarshalIndent(DynCfg, "", "  ")
	dynCfgMux.RUnlock()
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(".", "discord_config_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	_ = tmpFile.Chmod(0600)
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, ConfigFile); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// GetWelcomeChannelID vrací uvítací kanál (nejprve dynamický, pak fallback .env)
func GetWelcomeChannelID() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.DiscordWelcomeChannelID != "" {
		return DynCfg.DiscordWelcomeChannelID
	}
	return Cfg.DiscordWelcomeChannelID
}

// GetAuditChannelID vrací audit kanál
func GetAuditChannelID() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.DiscordAuditChannelID != "" {
		return DynCfg.DiscordAuditChannelID
	}
	return Cfg.DiscordAuditChannelID
}

// GetVerifiedRoleID vrací roli pro ověřené
func GetVerifiedRoleID() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.DiscordVerifiedRoleID != "" {
		return DynCfg.DiscordVerifiedRoleID
	}
	return Cfg.DiscordVerifiedID
}

// GetFMStudentRoleID vrací roli pro studenty
func GetFMStudentRoleID() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.DiscordFMStudentID != "" {
		return DynCfg.DiscordFMStudentID
	}
	return Cfg.DiscordFMStudentID
}

// GetFMStaffRoleID vrací roli pro zaměstnance
func GetFMStaffRoleID() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.DiscordFMStaffID != "" {
		return DynCfg.DiscordFMStaffID
	}
	return Cfg.DiscordFMStaffID
}

// GetWelcomeMessage vrací uvítací zprávu
func GetWelcomeMessage() string {
	dynCfgMux.RLock()
	defer dynCfgMux.RUnlock()
	if DynCfg.WelcomeMessage != "" {
		return DynCfg.WelcomeMessage
	}
	return Cfg.DiscordWelcomeMessage
}

// Nastavení hodnot s následným uložením
func SetWelcomeChannelID(id string) error {
	dynCfgMux.Lock()
	DynCfg.DiscordWelcomeChannelID = id
	dynCfgMux.Unlock()
	return SaveDynamic()
}

func SetAuditChannelID(id string) error {
	dynCfgMux.Lock()
	DynCfg.DiscordAuditChannelID = id
	dynCfgMux.Unlock()
	return SaveDynamic()
}

func SetRoleVerified(id string) error {
	dynCfgMux.Lock()
	DynCfg.DiscordVerifiedRoleID = id
	dynCfgMux.Unlock()
	return SaveDynamic()
}

func SetRoleStudent(id string) error {
	dynCfgMux.Lock()
	DynCfg.DiscordFMStudentID = id
	dynCfgMux.Unlock()
	return SaveDynamic()
}

func SetRoleStaff(id string) error {
	dynCfgMux.Lock()
	DynCfg.DiscordFMStaffID = id
	dynCfgMux.Unlock()
	return SaveDynamic()
}

func SetWelcomeMessage(msg string) error {
	dynCfgMux.Lock()
	DynCfg.WelcomeMessage = msg
	dynCfgMux.Unlock()
	return SaveDynamic()
}
