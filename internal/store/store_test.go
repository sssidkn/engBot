package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCurrentStreak(t *testing.T) {
	tests := []struct {
		name  string
		days  []string
		today string
		want  int
	}{
		{"empty", nil, "2026-09-05", 0},
		{"today only", []string{"2026-09-05"}, "2026-09-05", 1},
		{"yesterday keeps streak", []string{"2026-09-04"}, "2026-09-05", 1},
		{"broken", []string{"2026-09-01", "2026-09-03"}, "2026-09-05", 0},
		{"run ending today", []string{"2026-09-03", "2026-09-04", "2026-09-05"}, "2026-09-05", 3},
		{"run ending yesterday", []string{"2026-09-03", "2026-09-04"}, "2026-09-05", 2},
		{"gap then resume", []string{"2026-09-01", "2026-09-04", "2026-09-05"}, "2026-09-05", 2},
		{"bad today", []string{"2026-09-05"}, "not-a-date", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CurrentStreak(tt.days, tt.today); got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestBestStreak(t *testing.T) {
	days := []string{"2026-09-01", "2026-09-02", "2026-09-04", "2026-09-05", "2026-09-06"}
	if got := BestStreak(days); got != 3 {
		t.Fatalf("got %d want 3", got)
	}
	if got := BestStreak([]string{"bad"}); got != 0 {
		t.Fatalf("invalid first day got %d want 0", got)
	}
}

func TestChatTopic(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.ChatTopic(-100); got != 0 {
		t.Fatalf("empty topic=%d want 0", got)
	}
	if err := s.SetChatTopic(-100, 0); err != nil {
		t.Fatal(err)
	}
	if got := s.ChatTopic(-100); got != 0 {
		t.Fatalf("zero topic stored as %d", got)
	}
	if err := s.SetChatTopic(-100, 17); err != nil {
		t.Fatal(err)
	}
	if got := s.ChatTopic(-100); got != 17 {
		t.Fatalf("topic=%d want 17", got)
	}
}

func TestUnmarkTodayKeepsOtherDays(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(1, "", "Ann"); err != nil {
		t.Fatal(err)
	}
	today := s.Today(User{ID: 1, Timezone: "UTC"})
	s.mu.Lock()
	s.data.Checkins["1"] = []string{"2020-01-01", today, "2020-01-02"}
	s.mu.Unlock()

	removed, err := s.UnmarkToday(1)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected today to be removed")
	}
	days, err := s.Days(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("days=%v want 2 leftover days", days)
	}
	for _, d := range days {
		if d == today {
			t.Fatalf("today %s still in days after unmark: %v", today, days)
		}
	}
	has, err := s.HasDay(1, today)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("HasDay(today) after UnmarkToday")
	}

	added, err := s.MarkToday(1)
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected rematch after unmark")
	}
	days, _ = s.Days(1)
	if len(days) != 3 {
		t.Fatalf("after remark days=%v want 3", days)
	}
}

func TestUnmarkTodayNoAliasIntoLaterAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(9, "", "B"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkToday(9); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnmarkToday(9); err != nil {
		t.Fatal(err)
	}
	days, err := s.Days(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 0 {
		t.Fatalf("after unmark days=%v want empty", days)
	}
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.json")
	s, err := Open(path, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(1, "ann", "Ann"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(2, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s2.GetUser(2)
	if err != nil {
		t.Fatal(err)
	}
	if u.FirstName != "Bob" {
		t.Fatalf("reopen first_name=%q want Bob", u.FirstName)
	}
}

func TestOpenRecoversFromTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.json")
	payload := db{
		Users:    map[string]User{"7": {ID: 7, FirstName: "Recovered", Timezone: "UTC"}},
		Checkins: map[string][]string{},
		Chats:    map[string][]int64{},
		Topics:   map[string]int{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(7)
	if err != nil {
		t.Fatal(err)
	}
	if u.FirstName != "Recovered" {
		t.Fatalf("got %+v", u)
	}
}

func TestLocationEmptyAndInvalidTimezone(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	loc := s.Location(User{Timezone: ""})
	if loc.String() != "Europe/Moscow" {
		t.Fatalf("empty tz got %s want Europe/Moscow", loc)
	}
	loc = s.Location(User{Timezone: "Not/AZone"})
	if loc.String() != "Europe/Moscow" {
		t.Fatalf("invalid tz got %s want Europe/Moscow", loc)
	}
	loc = s.Location(User{Timezone: "UTC"})
	if loc.String() != "UTC" {
		t.Fatalf("utc got %s", loc)
	}
}

func TestSetTimezoneRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimezone(1, "   "); err == nil {
		t.Fatal("empty timezone should fail")
	}
	if err := s.SetTimezone(1, "Europe/Moscow"); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(1)
	if err != nil {
		t.Fatal(err)
	}
	if u.Timezone != "Europe/Moscow" {
		t.Fatalf("tz=%q", u.Timezone)
	}
}

func TestConcurrentMarkUnmarkAndTopic(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(1, "", "A"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, _ = s.MarkToday(1)
		}()
		go func() {
			defer wg.Done()
			_, _ = s.UnmarkToday(1)
		}()
		go func(n int) {
			defer wg.Done()
			_ = s.SetChatTopic(-100, n+1)
			_ = s.ChatTopic(-100)
		}(i)
	}
	wg.Wait()
	if _, err := s.Days(1); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.json")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(tmp, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q want new", got)
	}
}

func TestLocationEmbeddedZoneinfo(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Moscow"); err != nil {
		t.Fatalf("embedded tzdata missing: %v", err)
	}
}

func TestToggleDayPastAndFuture(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(1, "", "Ann"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimezone(1, "UTC"); err != nil {
		t.Fatal(err)
	}
	today := s.Today(User{ID: 1, Timezone: "UTC"})
	t0, err := time.Parse("2006-01-02", today)
	if err != nil {
		t.Fatal(err)
	}
	past := t0.AddDate(0, 0, -3).Format("2006-01-02")
	future := t0.AddDate(0, 0, 1).Format("2006-01-02")

	marked, err := s.ToggleDay(1, past)
	if err != nil || !marked {
		t.Fatalf("mark past: marked=%v err=%v", marked, err)
	}
	has, err := s.HasDay(1, past)
	if err != nil || !has {
		t.Fatalf("has past=%v err=%v", has, err)
	}
	marked, err = s.ToggleDay(1, past)
	if err != nil || marked {
		t.Fatalf("unmark past: marked=%v err=%v", marked, err)
	}
	_, err = s.ToggleDay(1, future)
	if err != ErrFutureDay {
		t.Fatalf("future err=%v want ErrFutureDay", err)
	}
	_, err = s.ToggleDay(1, "nope")
	if err != ErrBadDay {
		t.Fatalf("bad err=%v want ErrBadDay", err)
	}
}
