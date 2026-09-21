package discord

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

// Command je interface pro kazdy slash prikaz
type Command interface {
	// Info vrati definici prikazu pro Discord API
	Info() *discordgo.ApplicationCommand
	// Handle zpracuje interakci prikazu
	Handle(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// Registry uchovava registrovane prikazy
type Registry struct {
	commands map[string]Command
}

// NewRegistry vytvori novy registr prikazu
func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
	}
}

// Register zaregistruje novy prikaz
func (reg *Registry) Register(cmd Command) {
	name := cmd.Info().Name
	reg.commands[name] = cmd
	log.Printf("Registrován příkaz: /%s", name)
}

// SyncWithGuild synchronizuje registrovane prikazy s Discord guildem
func (reg *Registry) SyncWithGuild(s *discordgo.Session, guildID string) {
	for _, cmd := range reg.commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, guildID, cmd.Info())
		if err != nil {
			log.Printf("Chyba registrace příkazu /%s: %v", cmd.Info().Name, err)
		}
	}
	log.Printf("Synchronizováno %d příkazů s guildem", len(reg.commands))
}

// HandleInteraction nasmeruje interakci na spravny handler
func (reg *Registry) HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	name := i.ApplicationCommandData().Name
	if cmd, ok := reg.commands[name]; ok {
		cmd.Handle(s, i)
	}
}

// CommandCount vrati pocet registrovanych prikazu
func (reg *Registry) CommandCount() int {
	return len(reg.commands)
}

// HasCommand zjisti, zda je prikaz registrovan
func (reg *Registry) HasCommand(name string) bool {
	_, ok := reg.commands[name]
	return ok
}

// Globalni registr prikazu
var CmdRegistry = NewRegistry()
