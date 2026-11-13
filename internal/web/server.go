package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"sbibolet/internal/config"
	"sbibolet/internal/security"
)

//go:embed static
var staticFS embed.FS

// Server je HTTP server instance
var Server *http.Server

// Setup registruje HTTP handlery a spusti server
func Setup() {
	go refreshChannels() // predstahnout kanaly pro terminal na uvodni strance
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", HandleIndex) // jen presne "/", jinak by catch-all (napr. /favicon.ico) prepisoval OAuth state
	mux.HandleFunc("GET /go", HandleStart)
	mux.HandleFunc("GET /msal", HandleMSAL)
	mux.HandleFunc("GET /discord", HandleDiscord)
	mux.HandleFunc("GET /mock-msal", HandleMockMSAL)
	mux.HandleFunc("GET /mock-discord", HandleMockDiscord)
	mux.HandleFunc("GET /logout", HandleLogout)

	// Oficialni symbol FM TUL a fotky (vestavene v binarce)
	staticRoot, _ := fs.Sub(staticFS, "static")
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticRoot))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if strings.HasSuffix(r.URL.Path, ".woff2") {
			w.Header().Set("Content-Type", "font/woff2")
		}
		staticHandler.ServeHTTP(w, r)
	})

	// Obaleni vsech handleru bezpecnostnim middlewarem
	secureHandler := security.Middleware(mux)

	addr := fmt.Sprintf("%s:%s", config.Cfg.Host, config.Cfg.Port)
	Server = &http.Server{
		Addr:              addr,
		Handler:           secureHandler,
		ReadHeaderTimeout: 5 * time.Second,  // Prevence Slowloris utoku
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // Max 1 MB hlavicky
	}

	go func() {
		log.Printf("FM TUL Discord Connector běží na %s (lokálně: http://%s)", config.Cfg.BaseURL, addr)
		if err := Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Chyba HTTP serveru: %v", err)
		}
	}()
}

// Shutdown provede graceful shutdown HTTP serveru
func Shutdown() {
	if Server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = Server.Shutdown(ctx)
	}
}
