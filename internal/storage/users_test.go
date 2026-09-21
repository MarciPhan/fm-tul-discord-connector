package storage

import (
	"os"
	"testing"
	"time"
)

func TestAntiMultiAccounting(t *testing.T) {
	origFile := UsersStorageFile
	UsersStorageFile = "users_test_anti_cheat.json"
	ResetDB()
	defer func() {
		ResetDB()
		_ = os.Remove("users_test_anti_cheat.json")
		UsersStorageFile = origFile
	}()
	
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

	// Test zamezení vícenásobného účtu na základě stejného e-mailu (i kdyby se lišilo Microsoft ID)
	cheatAttemptEmail := &Student{
		MicrosoftID: "ms-student-different",
		DiscordID:   "discord-user-3",
		Name:        "Podvodník",
		Email:       "STUDENT1@TUL.CZ", // case-insensitive shoda
	}
	if err := CheckBindingAllowed(cheatAttemptEmail); err == nil {
		t.Error("Systém MUSÍ zamítnout registraci se shodným emailem pod jiným Discord ID")
	}
}

func TestConcurrentSaveBinding(t *testing.T) {
	ResetDB()
	UsersStorageFile = "users_test_tmp.json"
	defer func() {
		ResetDB()
		_ = os.Remove("users_test_tmp.json")
	}()

	var (
		msID     = "concurrent-ms-id"
		email    = "concurrent@tul.cz"
		workers  = 10
		successes int
		errs     int
	)

	// Spustíme více gorutin zkoušejících uložit stejný Microsoft ID pod různými Discord ID
	done := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			s := &Student{
				MicrosoftID: msID,
				DiscordID:   time.Now().Format("150405.000000") + string(rune('a'+idx)),
				Name:        "Concurrent User",
				Email:       email,
				VerifiedAt:  time.Now(),
			}
			done <- SaveUser(s)
		}(i)
	}

	for i := 0; i < workers; i++ {
		err := <-done
		if err == nil {
			successes++
		} else {
			errs++
		}
	}

	// Pouze právě JEDEN zápis smí uspět pod stejným MSID/emailem
	if successes != 1 {
		t.Errorf("Očekáván právě 1 úspěšný zápis, ale uspělo: %d (chyb: %d)", successes, errs)
	}
	if GetUserCount() != 1 {
		t.Errorf("Očekáván právě 1 uživatel v databázi, ale nalezeno: %d", GetUserCount())
	}
}
