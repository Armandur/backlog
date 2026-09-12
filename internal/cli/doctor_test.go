package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mazen160/backlog/internal/migrate"
	"github.com/mazen160/backlog/internal/models"
	"github.com/mazen160/backlog/internal/output"
	"github.com/mazen160/backlog/internal/repo"
	"github.com/mazen160/backlog/internal/service"
)

func TestDoctorBackupKanOppnasOchAterstallas(t *testing.T) {
	rot := t.TempDir()
	kallWorkspace := filepath.Join(rot, "kalla")
	if err := os.Mkdir(kallWorkspace, 0o700); err != nil {
		t.Fatalf("skapa källans workspace: %v", err)
	}

	kallDB, err := repo.Open(filepath.Join(kallWorkspace, "backlog.db"))
	if err != nil {
		t.Fatalf("öppna källdatabasen: %v", err)
	}
	t.Cleanup(func() { _ = kallDB.Close() })
	if err := migrate.Run(kallDB); err != nil {
		t.Fatalf("migrera källdatabasen: %v", err)
	}

	projektService := service.NewProjectService(kallDB)
	_, err = projektService.Create(context.Background(), models.CreateProjectInput{
		Alias: "sparat",
		Name:  "Sparat projekt",
		Actor: models.Actor{
			Kind: models.ActorKindHuman,
			Name: "testare",
		},
	})
	if err != nil {
		t.Fatalf("skapa projektet: %v", err)
	}

	gammalApp := app
	app = &App{DB: kallDB, WorkDir: kallWorkspace, Out: output.New(false, true)}
	t.Cleanup(func() { app = gammalApp })

	backupKatalog := filepath.Join(rot, "backuper")
	if err := os.Mkdir(backupKatalog, 0o700); err != nil {
		t.Fatalf("skapa backupkatalogen: %v", err)
	}
	backupSokvag := filepath.Join(backupKatalog, "pm.db")
	kommando := doctorBackupCmd()
	kommando.SetArgs([]string{"--to", backupSokvag})
	if err := kommando.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ta säkerhetskopian: %v", err)
	}

	kontrolleraBackup(t, backupSokvag)

	aterstalltWorkspace := filepath.Join(rot, "tomt-workspace")
	if err := os.Mkdir(aterstalltWorkspace, 0o700); err != nil {
		t.Fatalf("skapa tomt workspace: %v", err)
	}
	poster, err := os.ReadDir(aterstalltWorkspace)
	if err != nil {
		t.Fatalf("läs tomt workspace: %v", err)
	}
	if len(poster) != 0 {
		t.Fatalf("workspacet har %d filer före återställningen", len(poster))
	}

	backupData, err := os.ReadFile(backupSokvag)
	if err != nil {
		t.Fatalf("läs säkerhetskopian: %v", err)
	}
	aterstalldDB := filepath.Join(aterstalltWorkspace, "backlog.db")
	if err := os.WriteFile(aterstalldDB, backupData, 0o600); err != nil {
		t.Fatalf("återställ säkerhetskopian: %v", err)
	}

	kontrolleraBackup(t, aterstalldDB)
}

func kontrolleraBackup(t *testing.T, sokvag string) {
	t.Helper()
	db, err := repo.Open(sokvag)
	if err != nil {
		t.Fatalf("öppna %s: %v", sokvag, err)
	}
	defer db.Close()

	var integritet string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integritet); err != nil {
		t.Fatalf("kontrollera integriteten i %s: %v", sokvag, err)
	}
	if integritet != "ok" {
		t.Fatalf("integriteten i %s är %q, vill ha ok", sokvag, integritet)
	}

	var antal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE alias = ?`, "sparat").Scan(&antal); err != nil {
		t.Fatalf("läs projektet i %s: %v", sokvag, err)
	}
	if antal != 1 {
		t.Fatalf("%s innehåller %d sparade projekt, vill ha 1", sokvag, antal)
	}
}
