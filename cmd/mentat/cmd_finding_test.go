package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/mentat/internal/store/sqlite"
)

func TestFindingUpdateStatusCLI(t *testing.T) {
	dbDir := t.TempDir()
	dbPath = filepath.Join(dbDir, "test.db")

	output := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"finding", "add", "--title", "Example finding"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute add: %v", err)
		}
		rootCmd.SetArgs([]string{"finding", "update-status", "1", "resolved"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute update-status: %v", err)
		}
	})
	if !strings.Contains(output, "Finding #1 status updated: resolved") {
		t.Fatalf("update-status output missing success line: %s", output)
	}

	verifyStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer verifyStore.Close()

	findings, err := verifyStore.ListFindings("", "", "")
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("finding count = %d, want 1", len(findings))
	}
	if findings[0].Status != "resolved" {
		t.Fatalf("status = %q, want resolved", findings[0].Status)
	}

	var toolName string
	if err := verifyStore.DB().QueryRow(
		"SELECT COALESCE(tool_name, '') FROM findings WHERE id = ?", findings[0].ID,
	).Scan(&toolName); err != nil {
		t.Fatalf("query provenance: %v", err)
	}
	if toolName != "finding-update-status" {
		t.Fatalf("tool name = %q, want finding-update-status", toolName)
	}
}

func TestFindingUpdateStatusInvalidID(t *testing.T) {
	dbDir := t.TempDir()
	dbPath = filepath.Join(dbDir, "test.db")

	rootCmd.SetArgs([]string{"finding", "update-status", "0", "resolved"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for invalid finding id")
	}
}

func TestFindingUpdateStatusInvalidStatus(t *testing.T) {
	dbDir := t.TempDir()
	dbPath = filepath.Join(dbDir, "test.db")

	rootCmd.SetArgs([]string{"finding", "add", "--title", "Example finding"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute add: %v", err)
	}

	rootCmd.SetArgs([]string{"finding", "update-status", "1", "adressed"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unrecognized status")
	}
	if !strings.Contains(err.Error(), "invalid finding status") {
		t.Fatalf("error = %v, want it to mention the allowed set", err)
	}

	verifyStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer verifyStore.Close()

	findings, err := verifyStore.ListFindings("", "", "")
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("finding count = %d, want 1", len(findings))
	}
	if findings[0].Status != "open" {
		t.Fatalf("status = %q, want unchanged open", findings[0].Status)
	}
}
