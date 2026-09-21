package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type CommandDetailed struct {
	Name          string
	PermissionCS  string
	PermissionEN  string
	DescriptionCS string
	DescriptionEN string
	SourceFile    string
}

type EndpointInfo struct {
	Method  string
	Path    string
	Handler string
}

type CodeStats struct {
	TotalFiles int
	GoFiles    int
	TestFiles  int
	TotalLines int
}

func main() {
	fmt.Println("🚀 Spouštím automatickou aktualizaci Knowledge Base a README...")

	root := "."
	if _, err := os.Stat("internal"); os.IsNotExist(err) {
		root = ".."
	}

	commands := scanDetailedCommands(filepath.Join(root, "internal", "discord"))
	endpoints := scanHTTPEndpoints(filepath.Join(root, "internal", "web", "server.go"))
	stats := scanCodeStats(root)

	// 1. Aktualizace Knowledge Base souborů
	summaryPath := filepath.Join(root, "docs", "knowledge-base", "SUMMARY.md")
	aiContextPath := filepath.Join(root, "docs", "knowledge-base", "AI_CONTEXT.md")

	if err := generateSummary(summaryPath, commands, endpoints, stats); err != nil {
		fmt.Printf("❌ Chyba generování SUMMARY.md: %v\n", err)
		os.Exit(1)
	}

	if err := generateAIContext(aiContextPath, commands, endpoints, stats); err != nil {
		fmt.Printf("❌ Chyba generování AI_CONTEXT.md: %v\n", err)
		os.Exit(1)
	}

	// 2. Automatická aktualizace README.md a README_EN.md
	readmeCS := filepath.Join(root, "README.md")
	readmeEN := filepath.Join(root, "README_EN.md")

	if err := updateReadme(readmeCS, false, commands, stats); err != nil {
		fmt.Printf("❌ Chyba aktualizace README.md: %v\n", err)
		os.Exit(1)
	}
	if err := updateReadme(readmeEN, true, commands, stats); err != nil {
		fmt.Printf("❌ Chyba aktualizace README_EN.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Úspěšně aktualizováno:")
	fmt.Printf("   - %s\n", summaryPath)
	fmt.Printf("   - %s\n", aiContextPath)
	fmt.Printf("   - %s\n", readmeCS)
	fmt.Printf("   - %s\n", readmeEN)
}

func scanDetailedCommands(discordDir string) []CommandDetailed {
	var cmds []CommandDetailed
	files, err := os.ReadDir(discordDir)
	if err != nil {
		return cmds
	}

	reName := regexp.MustCompile(`Name:\s*"([^"]+)"`)
	reDesc := regexp.MustCompile(`Description:\s*"([^"]+)"`)
	reSubBlock := regexp.MustCompile(`Type:\s*discordgo\.ApplicationCommandOptionSubCommand[\s\S]*?Name:\s*"([^"]+)"[\s\S]*?Description:\s*"([^"]+)"`)

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "cmd_") && strings.HasSuffix(file.Name(), ".go") {
			path := filepath.Join(discordDir, file.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)

			// Oprávnění
			permCS := "Všichni členové"
			permEN := "Everyone"
			if strings.Contains(content, "PermissionAdministrator") {
				permCS = "Administrátor"
				permEN = "Administrator"
			} else if strings.Contains(content, "PermissionManageMessages") {
				permCS = "Správa zpráv"
				permEN = "Manage Messages"
			}

			nameMatch := reName.FindStringSubmatch(content)
			descMatch := reDesc.FindStringSubmatch(content)
			if len(nameMatch) < 2 {
				continue
			}

			mainName := nameMatch[1]
			mainDesc := "Bez popisu"
			if len(descMatch) > 1 {
				mainDesc = descMatch[1]
			}

			subMatches := reSubBlock.FindAllStringSubmatch(content, -1)
			if len(subMatches) > 0 {
				// Příkaz má podpříkazy
				for _, sub := range subMatches {
					subName := sub[1]
					subDesc := sub[2]
					fullName := fmt.Sprintf("/%s %s", mainName, subName)
					cmds = append(cmds, CommandDetailed{
						Name:          fullName,
						PermissionCS:  permCS,
						PermissionEN:  permEN,
						DescriptionCS: subDesc,
						DescriptionEN: translateCommandDesc(fullName, subDesc),
						SourceFile:    file.Name(),
					})
				}
			} else {
				fullName := "/" + mainName
				cmds = append(cmds, CommandDetailed{
					Name:          fullName,
					PermissionCS:  permCS,
					PermissionEN:  permEN,
					DescriptionCS: mainDesc,
					DescriptionEN: translateCommandDesc(fullName, mainDesc),
					SourceFile:    file.Name(),
				})
			}
		}
	}

	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})
	return cmds
}

func translateCommandDesc(cmd, csDesc string) string {
	switch cmd {
	case "/config set-channel":
		return "Configures the channel for selected feature (welcome or audit)"
	case "/config set-role":
		return "Configures target role for selected group (verified, student, staff)"
	case "/config set-message":
		return "Sets custom welcome message for newcomers (Markdown supported)"
	case "/config view":
		return "Displays current configuration state (from JSON and .env)"
	case "/delete":
		return "Safely deletes a message by its ID (managers only)"
	case "/edit":
		return "Edits a bot's own message in a channel (managers only)"
	case "/overit":
		return "Provides ephemeral verification link to the web portal"
	case "/rss add":
		return "Adds a new RSS feed for monitoring (SSRF protected)"
	case "/rss list":
		return "Lists all currently monitored RSS feeds"
	case "/rss remove":
		return "Removes a tracked RSS feed by ID"
	case "/say":
		return "Sends a message as the bot to a channel (managers only)"
	case "/setup":
		return "Sends official verification embed with button and reaction (admin only)"
	default:
		return csDesc
	}
}

func scanHTTPEndpoints(serverFile string) []EndpointInfo {
	var endpoints []EndpointInfo
	data, err := os.ReadFile(serverFile)
	if err != nil {
		return endpoints
	}

	reRoute := regexp.MustCompile(`mux\.HandleFunc\("([^"\s]+)\s+([^"]+)",\s*([^)]+)\)`)
	matches := reRoute.FindAllStringSubmatch(string(data), -1)
	for _, m := range matches {
		if len(m) >= 4 {
			endpoints = append(endpoints, EndpointInfo{
				Method:  m[1],
				Path:    m[2],
				Handler: m[3],
			})
		}
	}
	return endpoints
}

func scanCodeStats(root string) CodeStats {
	var stats CodeStats
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && (info.Name() == ".git" || info.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		stats.TotalFiles++
		if strings.HasSuffix(info.Name(), ".go") {
			stats.GoFiles++
			if strings.HasSuffix(info.Name(), "_test.go") {
				stats.TestFiles++
			}
			stats.TotalLines += countLines(path)
		}
		return nil
	})
	return stats
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	return lines
}

func replaceBetweenMarkers(content, startMarker, endMarker, newBlock string) string {
	idxStart := strings.Index(content, startMarker)
	if idxStart == -1 {
		return content
	}
	idxEnd := strings.Index(content, endMarker)
	if idxEnd == -1 || idxEnd < idxStart {
		return content
	}
	return content[:idxStart+len(startMarker)] + "\n" + newBlock + "\n" + content[idxEnd:]
}

func updateReadme(path string, isEnglish bool, cmds []CommandDetailed, stats CodeStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	// 1. Aktualizace tabulky příkazů
	var cmdTable strings.Builder
	if isEnglish {
		cmdTable.WriteString("| Command | Permission | Description |\n")
		cmdTable.WriteString("| :--- | :--- | :--- |\n")
		for _, c := range cmds {
			cmdTable.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", c.Name, c.PermissionEN, c.DescriptionEN))
		}
	} else {
		cmdTable.WriteString("| Příkaz | Výchozí oprávnění | Popis |\n")
		cmdTable.WriteString("| :--- | :--- | :--- |\n")
		for _, c := range cmds {
			cmdTable.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", c.Name, c.PermissionCS, c.DescriptionCS))
		}
	}

	content = replaceBetweenMarkers(content, "<!-- AUTO_COMMANDS_START -->", "<!-- AUTO_COMMANDS_END -->", strings.TrimRight(cmdTable.String(), "\n"))

	// 2. Aktualizace metrik
	var metricsBlock strings.Builder
	if isEnglish {
		metricsBlock.WriteString(fmt.Sprintf("[![Go Files](https://img.shields.io/badge/Go_Files-%d-00ADD8.svg)](https://go.dev/)\n", stats.GoFiles))
		metricsBlock.WriteString(fmt.Sprintf("[![Test Files](https://img.shields.io/badge/Tests-%d_Suites-22c55e.svg)](https://go.dev/)\n", stats.TestFiles))
		metricsBlock.WriteString(fmt.Sprintf("[![Code Size](https://img.shields.io/badge/Lines_of_Code-~%d-blue.svg)](https://go.dev/)\n", stats.TotalLines))
		metricsBlock.WriteString(fmt.Sprintf("[![Slash Commands](https://img.shields.io/badge/Slash_Commands-%d_Active-purple.svg)](https://discord.com/)", len(cmds)))
	} else {
		metricsBlock.WriteString(fmt.Sprintf("[![Go Soubory](https://img.shields.io/badge/Go_Soubory-%d-00ADD8.svg)](https://go.dev/)\n", stats.GoFiles))
		metricsBlock.WriteString(fmt.Sprintf("[![Testy](https://img.shields.io/badge/Testy-%d_Sad-22c55e.svg)](https://go.dev/)\n", stats.TestFiles))
		metricsBlock.WriteString(fmt.Sprintf("[![Velikost kódu](https://img.shields.io/badge/Řádků_kódu-~%d-blue.svg)](https://go.dev/)\n", stats.TotalLines))
		metricsBlock.WriteString(fmt.Sprintf("[![Slash Příkazy](https://img.shields.io/badge/Slash_Příkazy-%d_Aktivních-purple.svg)](https://discord.com/)", len(cmds)))
	}

	content = replaceBetweenMarkers(content, "<!-- AUTO_METRICS_START -->", "<!-- AUTO_METRICS_END -->", strings.TrimRight(metricsBlock.String(), "\n"))

	return os.WriteFile(path, []byte(content), 0644)
}

func generateSummary(path string, cmds []CommandDetailed, endpoints []EndpointInfo, stats CodeStats) error {
	var sb strings.Builder

	sb.WriteString("# Knowledge Base Index – FM TUL Discord Connector\n\n")
	sb.WriteString(fmt.Sprintf("*Automaticky vygenerováno: %s*\n\n", time.Now().Format("2006-01-02 15:04:05 MST")))

	sb.WriteString("## 📚 Dokumentace v Knowledge Base\n\n")
	sb.WriteString("- [Architektura systému](architecture.md) – Moduly, toky dat, background workery.\n")
	sb.WriteString("- [Bezpečnostní model](security.md) – HMAC sessions, PKCE, Origin validace, anti-multi-accounting, SSRF ochrana.\n")
	sb.WriteString("- [OAuth2 Průběh](oauth-flow.md) – Sekvenční diagram, Microsoft Entra ID a Discord OAuth2 integrace.\n")
	sb.WriteString("- [Slash Příkazy](slash-commands.md) – Registr a specifikace příkazů pro Discord.\n")
	sb.WriteString("- [Konfigurace](config-reference.md) – Kompletní matice proměnných `.env` a `discord_config.json`.\n")
	sb.WriteString("- [AI Context Snapshot](AI_CONTEXT.md) – Kompaktní přehled projektu pro rychlé načtení do kontextu AI.\n\n")

	sb.WriteString("## 📊 Metriky projektu\n\n")
	sb.WriteString(fmt.Sprintf("- **Celkem zdrojových Go souborů:** %d\n", stats.GoFiles))
	sb.WriteString(fmt.Sprintf("- **Testovací soubory:** %d\n", stats.TestFiles))
	sb.WriteString(fmt.Sprintf("- **Celkový počet řádků Go kódu:** ~%d\n", stats.TotalLines))
	sb.WriteString(fmt.Sprintf("- **Aktivních Slash příkazů a podpříkazů:** %d\n", len(cmds)))
	sb.WriteString(fmt.Sprintf("- **Aktivních HTTP endpointů:** %d\n\n", len(endpoints)))

	sb.WriteString("## 🤖 Slash Příkazy v repozitáři\n\n")
	sb.WriteString("| Příkaz | Oprávnění | Popis | Zdrojový soubor |\n| :--- | :--- | :--- | :--- |\n")
	for _, c := range cmds {
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | `internal/discord/%s` |\n", c.Name, c.PermissionCS, c.DescriptionCS, c.SourceFile))
	}
	sb.WriteString("\n")

	sb.WriteString("## 🌐 HTTP Endpointy (`internal/web/server.go`)\n\n")
	sb.WriteString("| Metoda | Cesta | Handler |\n| :--- | :--- | :--- |\n")
	for _, e := range endpoints {
		sb.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` |\n", e.Method, e.Path, e.Handler))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func generateAIContext(path string, cmds []CommandDetailed, endpoints []EndpointInfo, stats CodeStats) error {
	var sb strings.Builder

	sb.WriteString("# FM TUL Connector – AI Context Snapshot\n\n")
	sb.WriteString(fmt.Sprintf("*Generated at: %s*\n\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("## Core Invariants for AI Agents\n")
	sb.WriteString("1. Storage: Zero external SQL/Redis. Atomic JSON writes only (`users.json`, `feeds.json`, `discord_config.json`) with `0600` permissions.\n")
	sb.WriteString("2. Concurrency: Never hold locks across network calls. Use `Session.Mux` helper methods (`GetStudent`, `SetStudent`, `ConsumeMSALState`, `ConsumeDiscordState`).\n")
	sb.WriteString("3. Discord: Always use `GetInteractionUser(i)`, `GetInteractionUsername(i)`, `GetInteractionUserID(i)` to avoid nil pointer panics in DMs.\n")
	sb.WriteString("4. Security: HMAC-SHA256 session signatures, PKCE RFC 7636, Origin header validation, anti-multi-accounting 1:1 binding, SSRF validation for RSS.\n")
	sb.WriteString("5. Config precedence: `discord_config.json` > `.env`.\n\n")

	sb.WriteString("## Registered Commands\n")
	for _, c := range cmds {
		sb.WriteString(fmt.Sprintf("- `%s` (%s): %s (`%s`)\n", c.Name, c.PermissionEN, c.DescriptionEN, c.SourceFile))
	}
	sb.WriteString("\n## HTTP Routes\n")
	for _, e := range endpoints {
		sb.WriteString(fmt.Sprintf("- `%s %s` -> `%s`\n", e.Method, e.Path, e.Handler))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}
