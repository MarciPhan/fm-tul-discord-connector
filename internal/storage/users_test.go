package storage

import (
	"testing"
	"time"
)

func TestAntiMultiAccounting(t *testing.T) {
	ResetDB()
	
	validStudent := &Student{
		MicrosoftID: "ms-student-1",
		DiscordID:   "discord-user-1",
		Name:        "Student Jedna",
		Email:       "student1@tul.cz",
		VerifiedAt:  time.Now(),
	}
	
	// Uložíme prvního uživatele
	_ = SaveUser(validStudent)

	// Pokus ověřit STEJNÉ Microsoft ID s JINÝM Discord ID (sdílení školního účtu s kamarádem)
	cheatAttempt1 := &Student{
		MicrosoftID: "ms-student-1",
		DiscordID:   "discord-user-2-kamarad",
		Name:        "Kamarád",
		Email:       "student1@tul.cz",
	}
	if err := CheckBindingAllowed(cheatAttempt1); err == nil {
		t.Error("Systém MUSÍ zamítnout ověření jiného Discord účtu se stejným Microsoft ID")
	}

	// Pokus ověřit STEJNÝ Discord účet s JINÝM Microsoft ID
	cheatAttempt2 := &Student{
		MicrosoftID: "ms-student-2",
		DiscordID:   "discord-user-1",
		Name:        "Student Dva",
		Email:       "student2@tul.cz",
	}
	if err := CheckBindingAllowed(cheatAttempt2); err == nil {
		t.Error("Systém MUSÍ zamítnout přepsání již ověřeného Discord účtu jiným Microsoft ID")
	}

	// Znovusparovani stejneho uctu musi projit
	if err := CheckBindingAllowed(validStudent); err != nil {
		t.Errorf("Reautentizace téhož účtu musí projít, chyba: %v", err)
	}
}
