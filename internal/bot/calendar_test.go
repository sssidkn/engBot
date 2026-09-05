package bot

import (
	"strings"
	"testing"
	"time"

	"engbot/internal/store"
)

func TestParseYearMonth(t *testing.T) {
	y, m, err := parseYearMonth("2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if y != 2026 || m != time.September {
		t.Fatalf("got %d %s", y, m)
	}
}

func TestClampMonth(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	y, m := clampMonth(2030, 1, now)
	if y != 2026 || m != time.September {
		t.Fatalf("future clamp %d %s", y, m)
	}
	y, m = clampMonth(2000, 1, now)
	if y != 2023 || m != time.September {
		t.Fatalf("past clamp %d %s want 2023 September", y, m)
	}
}

func TestDayButtonText(t *testing.T) {
	if got := dayButtonText(5, store.DayStudied); got != "✅5" {
		t.Fatalf("studied=%s", got)
	}
	if got := dayButtonText(8, store.DayMissed); got != "❌8" {
		t.Fatalf("missed=%s", got)
	}
	if got := dayButtonText(5, store.DayToday); got != "•5" {
		t.Fatalf("today=%s", got)
	}
	if got := dayButtonText(12, store.DayEmpty); got != "12" {
		t.Fatalf("empty=%s", got)
	}
}

func TestCalendarMarkupWeekStartsMonday(t *testing.T) {
	loc := time.UTC
	view := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, loc)
	grid := map[int]store.DayStatus{
		1: store.DayStudied,
		2: store.DayMissed,
		5: store.DayToday,
	}
	m := calendarMarkup(view, now, grid)
	if m == nil || len(m.InlineKeyboard) < 3 {
		t.Fatalf("rows=%v", m)
	}
	head := m.InlineKeyboard[0]
	if len(head) != 7 || head[0].Text != "пн" || head[6].Text != "вс" {
		t.Fatalf("header=%v", head)
	}
	week1 := m.InlineKeyboard[1]
	if len(week1) != 7 {
		t.Fatalf("week1 len=%d", len(week1))
	}
	var cells []string
	for _, b := range week1 {
		cells = append(cells, b.Text)
	}
	if week1[1].Text != "✅1" { // 1 Sep 2026 is Tuesday
		t.Fatalf("week1=%v want pad then ✅1", cells)
	}
	if week1[2].Text != "❌2" {
		t.Fatalf("week1=%v", cells)
	}
	nav := m.InlineKeyboard[len(m.InlineKeyboard)-2]
	if len(nav) != 3 || nav[0].Text != "◀" {
		t.Fatalf("nav=%v", nav)
	}
	if nav[2].Text != " " {
		t.Fatalf("next should be disabled in current month, got %q", nav[2].Text)
	}
	back := m.InlineKeyboard[len(m.InlineKeyboard)-1]
	if len(back) != 1 || back[0].Text != "← Меню" || back[0].Unique != calMenuUnique {
		t.Fatalf("back=%v", back)
	}
}

func TestCalendarCaptionCounts(t *testing.T) {
	view := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	grid := map[int]store.DayStatus{1: store.DayStudied, 2: store.DayMissed, 5: store.DayToday}
	text := calendarCaption(view, grid, store.Stats{CurrentStreak: 3})
	if !strings.Contains(text, "Сентябрь 2026") {
		t.Fatalf("title: %s", text)
	}
	if !strings.Contains(text, "занятий: <b>1</b>") || !strings.Contains(text, "пропусков: <b>1</b>") {
		t.Fatalf("counts: %s", text)
	}
	if !strings.Contains(text, "серия: <b>3</b>") {
		t.Fatalf("streak: %s", text)
	}
}

func TestCalendarMarkupPrevMonthHasNext(t *testing.T) {
	loc := time.UTC
	view := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, loc)
	m := calendarMarkup(view, now, map[int]store.DayStatus{})
	nav := m.InlineKeyboard[len(m.InlineKeyboard)-2]
	if nav[2].Text != "▶" {
		t.Fatalf("expected next arrow, got %q", nav[2].Text)
	}
	if !strings.Contains(nav[2].Data, "2026-09") && nav[2].Unique != calNavUnique {
		// telebot stores payload in Data; Unique is caln
	}
	if nav[2].Unique != calNavUnique {
		t.Fatalf("unique=%s", nav[2].Unique)
	}
}
