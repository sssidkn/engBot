package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenOK(t *testing.T) {
	if tokenOK("") || tokenOK("   ") || tokenOK(placeholderToken) {
		t.Fatal("empty or placeholder token must be rejected")
	}
	if tokenOK(" " + placeholderToken + " ") {
		t.Fatal("padded placeholder must be rejected")
	}
	if !tokenOK("123456:real-looking-but-not-logged") {
		t.Fatal("non-empty custom token should pass format check")
	}
}

func TestFindEnvFileNearestOnly(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("ENGBOT_TEST_PARENT=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".env"), []byte("ENGBOT_TEST_CHILD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	got := findEnvFile()
	want := filepath.Join(child, ".env")
	if got != want {
		t.Fatalf("findEnvFile=%q want nearest %q", got, want)
	}
}

func TestFindEnvFileNoneInIsolatedTree(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got := findEnvFile()
	if got == "" {
		return
	}
	if rel, err := filepath.Rel(dir, got); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("unexpected env file inside isolated dir: %s", got)
	}
}

func TestSetupLoggingWritesFile(t *testing.T) {
	prev := log.Writer()
	flags := log.Flags()
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	path := filepath.Join(t.TempDir(), "engBot.log")
	f, err := setupLogging(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("hello-log-test")
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "hello-log-test") {
		t.Fatalf("log file missing line: %s", got)
	}
	if !strings.Contains(string(got), "логи пишутся") {
		t.Fatalf("log file missing setup line: %s", got)
	}
}

func TestGetenvTrim(t *testing.T) {
	t.Setenv("ENGBOT_TEST_GETENV", "  Moscow  ")
	if got := getenv("ENGBOT_TEST_GETENV", "UTC"); got != "Moscow" {
		t.Fatalf("got %q", got)
	}
	if got := getenv("ENGBOT_TEST_MISSING", "UTC"); got != "UTC" {
		t.Fatalf("got %q", got)
	}
}
