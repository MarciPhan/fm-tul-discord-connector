package audit

import (
	"log"
	"time"
)

// Level určuje typ a barvu logu v Discordu
type Level string

const (
	LevelInfo    Level = "INFO"    // Modrá
	LevelSuccess Level = "SUCCESS" // Zelená
	LevelWarning Level = "WARNING" // Oranžová
	LevelDanger  Level = "DANGER"  // Červená
)

type Event struct {
	Level       Level
	Title       string
	Description string
	Timestamp   time.Time
}

// Fronta pro logy. Je dostatečně velká, aby nedošlo k blokování.
var eventChan = make(chan Event, 200)

// Log odešle událost do fronty pro zpracování Discord botem.
// Neblokuje hlavní vlákno (pokud je fronta plná, log se zahodí,
// aby nedošlo k pádu serveru, ale vypíše se do konzole).
func Log(level Level, title, desc string) {
	evt := Event{
		Level:       level,
		Title:       title,
		Description: desc,
		Timestamp:   time.Now(),
	}

	select {
	case eventChan <- evt:
		// Úspěšně zařazeno do fronty
	default:
		// Fronta je plná (extrém), logujeme aspoň do konzole
		log.Printf("[AUDIT DROP - QUEUE FULL] %s: %s", title, desc)
	}
}

// GetChannel vrací read-only kanál událostí pro worker.
func GetChannel() <-chan Event {
	return eventChan
}
