package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"sbibolet/internal/config"
	"sbibolet/internal/discord"
	"sbibolet/internal/session"
	"sbibolet/internal/storage"
	"sbibolet/internal/web"
)

func main() {
	// 1. Načtení konfigurace z .env
	config.Load()

	// 2. Načtení stávajících ověřených uživatelů z disku
	storage.LoadUsers()

	// 3. Spuštění periodického čištění sessions a rate limitů
	stopCleaner := session.StartCleaner()

	// 4. Spuštění Discord bota (s registrem příkazů, reakcemi, welcome)
	discord.Setup()

	// 5. Spuštění HTTP serveru (OAuth2 flow)
	web.Setup()

	// 6. Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Zastavuji server a Discord bota...")
	web.Shutdown()
	discord.Close()
	stopCleaner()
	log.Println("Aplikace byla korektně ukončena.")
}
