package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseClock(t *testing.T) {
	got, err := ParseClock("9:00")
	if err != nil || got != "09:00" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = ParseClock("21.30")
	if err != nil || got != "21:30" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := ParseClock("24:00"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseClock(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestAddAndDueReminders(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(1, "ann", "Ann"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimezone(1, "UTC"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 19, 0, 20, 0, time.UTC)
	list, err := s.AddReminder(1, "19:00", -100, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != "19:00" {
		t.Fatalf("list=%v", list)
	}
	_, err = s.AddReminder(1, "7:15", -100, 12)
	if err != nil {
		t.Fatal(err)
	}
	dues := s.DueReminders(now)
	if len(dues) != 1 || dues[0].Clock != "19:00" || dues[0].ChatID != -100 || dues[0].Thread != 12 {
		t.Fatalf("dues=%+v", dues)
	}
	if dues[0].User.Username != "ann" {
		t.Fatalf("username=%s", dues[0].User.Username)
	}
	if err := s.MarkReminded(1, "19:00", "2026-09-05"); err != nil {
		t.Fatal(err)
	}
	if n := s.DueReminders(now); len(n) != 0 {
		t.Fatalf("already fired: %+v", n)
	}
}

func TestDueRemindersSkipIfStudied(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(2, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimezone(2, "UTC"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	if _, err := s.AddReminder(2, now.Format("15:04"), 2, 0); err != nil {
		t.Fatal(err)
	}
	today := s.Today(User{ID: 2, Timezone: "UTC"})
	if _, err := s.ToggleDay(2, today); err != nil {
		t.Fatal(err)
	}
	if dues := s.DueReminders(now); len(dues) != 0 {
		t.Fatalf("studied still due: %+v", dues)
	}
}

func TestMaxReminders(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(3, "", "C"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxReminders; i++ {
		clock := time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format("15:04")
		if _, err := s.AddReminder(3, clock, 1, 0); err != nil {
			t.Fatalf("add %s: %v", clock, err)
		}
	}
	if _, err := s.AddReminder(3, "23:59", 1, 0); err != ErrTooManyReminders {
		t.Fatalf("err=%v", err)
	}
}
