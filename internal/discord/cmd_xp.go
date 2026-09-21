package discord

// ============================================================================
// XP SYSTÉM – STUB PRO BUDOUCÍ IMPLEMENTACI
// ============================================================================
//
// Připravená struktura pro XP (zkušenostní body) systém.
// Pro aktivaci implementujte logiku a přidejte do .env:
//   XP_ENABLED="true"
//
// Plánované funkce:
// - Uživatelé získávají XP za zprávy (s cooldownem)
// - Levelování s konfigurovatelnou křivkou
// - /xp příkaz pro zobrazení vlastního XP a levelu
// - /leaderboard pro žebříček top uživatelů
// - Automatické přidělení rolí při dosažení levelu
//
// TODO: Implementovat XP handler pro MessageCreate event
// TODO: Implementovat perzistenci XP (rozšíření users.json nebo SQLite)
// TODO: Implementovat /xp slash command
// TODO: Implementovat /leaderboard slash command

// import (
// 	"github.com/bwmarrin/discordgo"
// )
//
// type CmdXP struct{}
//
// func (c *CmdXP) Info() *discordgo.ApplicationCommand {
// 	return &discordgo.ApplicationCommand{
// 		Name:        "xp",
// 		Description: "Zobrazí tvůj aktuální XP a level",
// 	}
// }
//
// func (c *CmdXP) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
// 	respondEphemeral(s, i, "🚧 XP systém je v přípravě.")
// }
//
// func init() {
// 	CmdRegistry.Register(&CmdXP{})
// }
