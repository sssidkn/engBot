package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRotatorBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engBot.log")
	w, err := newLogRotator(path, 64, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write(bytes.Repeat([]byte("x"), 80)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("next-line\n")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var archived bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "engBot-") && strings.HasSuffix(e.Name(), ".log") {
			archived = true
		}
	}
	if !archived {
		t.Fatalf("expected archived log, files=%v", names(entries))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "next-line") {
		t.Fatalf("active log=%q", got)
	}
}

func TestBackupDBOncePerDay(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "engbot.json")
	if err := os.WriteFile(db, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p1, created, err := backupDB(db, "UTC")
	if err != nil || !created {
		t.Fatalf("first backup created=%v err=%v", created, err)
	}
	raw, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("backup body=%s", raw)
	}
	p2, created, err := backupDB(db, "UTC")
	if err != nil || created {
		t.Fatalf("second backup created=%v err=%v", created, err)
	}
	if p1 != p2 {
		t.Fatalf("path changed %s -> %s", p1, p2)
	}
}

func TestPruneFilesKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"engbot-2026-01-01.json", "engbot-2026-01-02.json", "engbot-2026-01-03.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneFiles(dir, "engbot-", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "engbot-2026-01-01.json")); !os.IsNotExist(err) {
		t.Fatal("oldest should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "engbot-2026-01-03.json")); err != nil {
		t.Fatal("newest should stay")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
