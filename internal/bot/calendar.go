package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"engbot/internal/store"

	tele "gopkg.in/telebot.v3"
)

const (
	calNavUnique  = "caln"
	calDayUnique  = "cald"
	calNoopUnique = "calz"
	calMenuUnique = "calm"
)

var monthNamesRU = []string{
	"", "январь", "февраль", "март", "апрель", "май", "июнь",
	"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь",
}

func (a *App) handleCalendar(c tele.Context, u store.User) error {
	loc := a.Store.Location(u)
	now := time.Now().In(loc)
	return a.showCalendar(c, u, now.Year(), now.Month())
}

func (a *App) handleCalNav(c tele.Context, u store.User) error {
	y, m, err := parseYearMonth(c.Data())
	if err != nil {
		log.Printf("cal nav bad data user=%d data=%q", u.ID, c.Data())
		return respondText(c, "Не получилось открыть месяц.")
	}
	y, m = clampMonth(y, m, time.Now().In(a.Store.Location(u)))
	return a.showCalendar(c, u, y, m)
}

func (a *App) handleCalDay(c tele.Context, u store.User) error {
	day := strings.TrimSpace(c.Data())
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return respondText(c, "Неизвестный день.")
	}
	marked, err := a.Store.ToggleDay(u.ID, day)
	if err != nil {
		if err == store.ErrFutureDay {
			return respondText(c, "Этот день ещё не наступил.")
		}
		if err == store.ErrBadDay {
			return respondText(c, "Неизвестный день.")
		}
		log.Printf("cal toggle user=%d day=%s: %v", u.ID, day, err)
		return respondText(c, "Не получилось обновить день.")
	}
	if marked {
		log.Printf("cal mark user=%d day=%s", u.ID, day)
	} else {
		log.Printf("cal unmark user=%d day=%s", u.ID, day)
	}
	return a.showCalendar(c, u, t.Year(), t.Month())
}

func (a *App) handleCalMenu(c tele.Context, _ store.User) error {
	return a.replyOrEdit(c, menuText(), mainMenu())
}

func (a *App) handleCalNoop(c tele.Context) error {
	return respondText(c, "")
}

func (a *App) showCalendar(c tele.Context, u store.User, year int, month time.Month) error {
	loc := a.Store.Location(u)
	now := time.Now().In(loc)
	year, month = clampMonth(year, month, now)
	view := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	grid, err := a.Store.MonthGrid(u.ID, year, month)
	if err != nil {
		log.Printf("calendar user=%d: %v", u.ID, err)
		return a.reply(c, "Не получилось построить календарь.")
	}
	st, err := a.Store.Stats(u.ID)
	if err != nil {
		log.Printf("calendar stats user=%d: %v", u.ID, err)
		st = store.Stats{}
	}
	text := calendarCaption(view, grid, st)
	markup := calendarMarkup(view, now, grid)
	return a.replyOrEdit(c, text, markup)
}

func calendarCaption(view time.Time, grid map[int]store.DayStatus, st store.Stats) string {
	ok, miss := 0, 0
	for _, status := range grid {
		switch status {
		case store.DayStudied:
			ok++
		case store.DayMissed:
			miss++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📅 <b>%s %d</b>\n", capitalize(monthNamesRU[view.Month()]), view.Year())
	fmt.Fprintf(&b, "✅ занятий: <b>%d</b>", ok)
	if miss > 0 {
		fmt.Fprintf(&b, "   ❌ пропусков: <b>%d</b>", miss)
	}
	b.WriteByte('\n')
	if st.CurrentStreak > 0 {
		fmt.Fprintf(&b, "🔥 серия: <b>%d</b> %s\n", st.CurrentStreak, dayWord(st.CurrentStreak))
	}
	b.WriteString("\nНажми любой день (не будущий), чтобы отметить занятие или снять отметку.")
	b.WriteString("\n\n✅ занятие   ❌ пропуск   • сегодня   · нет отметки")
	return b.String()
}

func calendarMarkup(view, now time.Time, grid map[int]store.DayStatus) *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	weekdays := []string{"пн", "вт", "ср", "чт", "пт", "сб", "вс"}
	head := make([]tele.Btn, 7)
	for i, w := range weekdays {
		head[i] = menu.Data(w, calNoopUnique, "h")
	}
	rows := []tele.Row{menu.Row(head...)}

	first := time.Date(view.Year(), view.Month(), 1, 0, 0, 0, 0, view.Location())
	lastDay := first.AddDate(0, 1, -1).Day()
	pad := int(first.Weekday())
	if pad == 0 {
		pad = 6
	} else {
		pad--
	}

	week := make([]tele.Btn, 0, 7)
	for i := 0; i < pad; i++ {
		week = append(week, menu.Data(" ", calNoopUnique, "p"))
	}
	for d := 1; d <= lastDay; d++ {
		key := time.Date(view.Year(), view.Month(), d, 0, 0, 0, 0, view.Location()).Format("2006-01-02")
		week = append(week, menu.Data(dayButtonText(d, grid[d]), calDayUnique, key))
		if len(week) == 7 {
			rows = append(rows, menu.Row(append([]tele.Btn(nil), week...)...))
			week = make([]tele.Btn, 0, 7)
		}
	}
	if len(week) > 0 {
		for len(week) < 7 {
			week = append(week, menu.Data(" ", calNoopUnique, "p"))
		}
		rows = append(rows, menu.Row(append([]tele.Btn(nil), week...)...))
	}

	prev := first.AddDate(0, -1, 0)
	next := first.AddDate(0, 1, 0)
	nowMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	left := menu.Data("◀", calNavUnique, prev.Format("2006-01"))
	title := menu.Data(fmt.Sprintf("%s %d", shortMonth(view.Month()), view.Year()), calNoopUnique, "t")
	var right tele.Btn
	if !next.After(nowMonth) {
		right = menu.Data("▶", calNavUnique, next.Format("2006-01"))
	} else {
		right = menu.Data(" ", calNoopUnique, "n")
	}
	rows = append(rows, menu.Row(left, title, right))
	rows = append(rows, menu.Row(menu.Data("← Меню", calMenuUnique, "m")))
	menu.Inline(rows...)
	return menu
}

func dayButtonText(day int, st store.DayStatus) string {
	switch st {
	case store.DayStudied:
		return fmt.Sprintf("✅%d", day)
	case store.DayMissed:
		return fmt.Sprintf("❌%d", day)
	case store.DayToday:
		return fmt.Sprintf("•%d", day)
	default:
		return fmt.Sprintf("%d", day)
	}
}

func parseYearMonth(s string) (int, time.Month, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(s))
	if err != nil {
		return 0, 0, err
	}
	return t.Year(), t.Month(), nil
}

func clampMonth(year int, month time.Month, now time.Time) (int, time.Month) {
	view := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	earliest := latest.AddDate(0, -36, 0)
	if view.After(latest) {
		return latest.Year(), latest.Month()
	}
	if view.Before(earliest) {
		return earliest.Year(), earliest.Month()
	}
	return view.Year(), view.Month()
}

func shortMonth(m time.Month) string {
	names := []string{"", "янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
	if int(m) < 1 || int(m) > 12 {
		return ""
	}
	return names[m]
}

func respondText(c tele.Context, text string) error {
	if c == nil || c.Callback() == nil {
		return nil
	}
	opt := &tele.CallbackResponse{}
	if text != "" {
		opt.Text = text
	}
	if err := c.Respond(opt); err != nil {
		log.Printf("callback respond: %v", err)
		return err
	}
	return nil
}

func (a *App) replyOrEdit(c tele.Context, text string, extra ...interface{}) error {
	if c != nil && c.Callback() != nil && c.Message() != nil {
		if err := c.Respond(); err != nil {
			log.Printf("callback respond: %v", err)
		}
		tid := 0
		if a != nil && a.Store != nil {
			if chat := c.Chat(); chat != nil {
				tid = effectiveThreadID(c, a.Store.ChatTopic(chat.ID))
			}
		} else {
			tid = threadID(c)
		}
		if err := c.Edit(text, append([]interface{}{tele.ModeHTML}, extra...)...); err != nil {
			if strings.Contains(err.Error(), "message is not modified") {
				return nil
			}
			log.Printf("edit failed, sending new: %v", err)
			return replyWith(c, tid, text, extra...)
		}
		return nil
	}
	return a.reply(c, text, extra...)
}
