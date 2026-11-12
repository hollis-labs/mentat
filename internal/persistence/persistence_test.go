package persistence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckNeverCreatesAndSaysWhatToDo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mentat.db")
	if err := Check(path, ReadExisting); err == nil || !strings.Contains(err.Error(), "Nothing was created") {
		t.Fatalf("missing file, read: %v", err)
	}
	if err := Check(path, Create); err != nil {
		t.Fatalf("missing file, create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, LegacyFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, how := range []Open{ReadExisting, Create} {
		if err := Check(path, how); err == nil || !strings.Contains(err.Error(), "migrate-legacy") {
			t.Fatalf("beside the old database (%d): %v", how, err)
		}
	}
	if err := Check(path, CreateForMigration); err != nil {
		t.Fatalf("migration may create beside the old database: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("Check created something: %v", entries)
	}
}
