package bot

import (
	"fmt"
	"html"
	"log"
	"strings"
	"time"

	"engbot/internal/store"

	tele "gopkg.in/telebot.v3"
)

const (
	rmdDelUnique = "rmdd"
	rmdClrUnique = "rmdc"
)

func (a *App) handleRemind(c tele.Context, u store.User) error {
	arg := ""
	if c != nil && c.Callback() == nil {
		arg = strings.TrimSpace(c.Data())
	}
	chatID, thread := notifyFromContext(c)
	if arg == "" {
		if chatID != 0 {
			_ = a.Store.SetNotifyTarget(u.ID, chatID, thread)
		}
		return a.showReminders(c, u.ID)
	}
	low := strings.ToLower(arg)
	if low == "off" || low == "clear" || low == "стоп" {
		if err := a.Store.ClearReminders(u.ID); err != nil {
			log.Printf("remind clear user=%d: %v", u.ID, err)
			return a.reply(c, "Не получилось сбросить напоминания.")
		}
		log.Printf("remind clear user=%d", u.ID)
		return a.reply(c, "Все напоминания выключены.", remindMarkup(nil))
	}
	if strings.HasPrefix(low, "del ") || strings.HasPrefix(low, "удали ") {
		parts := strings.Fields(arg)
		if len(parts) < 2 {
			return a.reply(c, "Пример: <code>/remind del 19:00</code>")
		}
		list, err := a.Store.RemoveReminder(u.ID, parts[1])
		if err != nil {
			return a.reply(c, "Не получилось разобрать время. Пример: <code>/remind del 19:00</code>")
		}
		log.Printf("remind del user=%d clock=%s", u.ID, parts[1])
		return a.reply(c, remindText(list, chatID != 0), remindMarkup(list))
	}
	var added []string
	for _, raw := range strings.Fields(arg) {
		list, err := a.Store.AddReminder(u.ID, raw, chatID, thread)
		if err == store.ErrTooManyReminders {
			return a.reply(c, fmt.Sprintf("Можно не больше %d напоминаний.", store.MaxReminders), remindMarkup(list))
		}
		if err != nil {
			return a.reply(c, "Время в формате <code>ЧЧ:ММ</code>. Можно несколько: <code>/remind 09:00 21:30</code>")
		}
		added = list
	}
	log.Printf("remind add user=%d times=%v chat=%d thread=%d", u.ID, added, chatID, thread)
	return a.reply(c, remindText(added, true), remindMarkup(added))
}

func (a *App) handleRemindButton(c tele.Context, u store.User) error {
	if chatID, thread := notifyFromContext(c); chatID != 0 {
		_ = a.Store.SetNotifyTarget(u.ID, chatID, thread)
	}
	return a.showReminders(c, u.ID)
}

func (a *App) handleRemindDel(c tele.Context, u store.User) error {
	list, err := a.Store.RemoveReminder(u.ID, c.Data())
	if err != nil {
		log.Printf("remind del btn user=%d: %v", u.ID, err)
		return respondText(c, "Не получилось удалить.")
	}
	log.Printf("remind del btn user=%d clock=%s", u.ID, c.Data())
	return a.replyOrEdit(c, remindText(list, true), remindMarkup(list))
}

func (a *App) handleRemindClear(c tele.Context, u store.User) error {
	if err := a.Store.ClearReminders(u.ID); err != nil {
		log.Printf("remind clear btn user=%d: %v", u.ID, err)
		return respondText(c, "Не получилось сбросить.")
	}
	log.Printf("remind clear btn user=%d", u.ID)
	return a.replyOrEdit(c, "Все напоминания выключены.", remindMarkup(nil))
}

func (a *App) showReminders(c tele.Context, userID int64) error {
	list := a.Store.Reminders(userID)
	return a.replyOrEdit(c, remindText(list, true), remindMarkup(list))
}

func remindText(times []string, bound bool) string {
	var b strings.Builder
	b.WriteString("⏰ <b>Напоминания</b>\n")
	if len(times) == 0 {
		b.WriteString("Пока нет. Добавь время в своём поясе:\n<code>/remind 09:00</code>\n<code>/remind 09:00 21:30</code>")
		return b.String()
	}
	b.WriteString("Сегодня напомню в:\n")
	for _, t := range times {
		fmt.Fprintf(&b, "• <code>%s</code>\n", html.EscapeString(t))
	}
	b.WriteString("\nНажми время, чтобы удалить. Если сегодня уже есть отметка — писать не буду.")
	if bound {
		b.WriteString("\nСообщение придёт в этот чат (в группе — с юзернеймом).")
	}
	return b.String()
}

func remindMarkup(times []string) *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	var rows []tele.Row
	row := make([]tele.Btn, 0, 3)
	for _, t := range times {
		row = append(row, menu.Data("✕ "+t, rmdDelUnique, t))
		if len(row) == 3 {
			rows = append(rows, menu.Row(append([]tele.Btn(nil), row...)...))
			row = make([]tele.Btn, 0, 3)
		}
	}
	if len(row) > 0 {
		rows = append(rows, menu.Row(append([]tele.Btn(nil), row...)...))
	}
	if len(times) > 0 {
		rows = append(rows, menu.Row(menu.Data("Выключить все", rmdClrUnique, "x")))
	}
	rows = append(rows, menu.Row(menu.Data("← Меню", calMenuUnique, "m")))
	menu.Inline(rows...)
	return menu
}

func notifyFromContext(c tele.Context) (chatID int64, thread int) {
	if c == nil {
		return 0, 0
	}
	if chat := c.Chat(); chat != nil {
		chatID = chat.ID
	}
	return chatID, threadID(c)
}

func mentionHTML(u store.User) string {
	label := strings.TrimSpace(u.FirstName)
	if u.Username != "" {
		label = "@" + u.Username
	}
	if label == "" {
		label = "ты"
	}
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, u.ID, html.EscapeString(label))
}

func reminderMessage(u store.User, clock string) string {
	return fmt.Sprintf("%s, Эй дебилкаааа смотри кого потеряла ⏰\nНапоминание на <b>%s</b>.",
		mentionHTML(u), html.EscapeString(clock))
}

func (a *App) RunReminders() {
	if a == nil || a.Bot == nil || a.Store == nil {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	a.tickReminders(time.Now())
	for range ticker.C {
		a.tickReminders(time.Now())
	}
}

func (a *App) tickReminders(now time.Time) {
	if a == nil || a.Store == nil || a.Bot == nil {
		return
	}
	dues := a.Store.DueReminders(now)
	for _, due := range dues {
		day := now.In(a.Store.Location(due.User)).Format("2006-01-02")
		if err := a.Store.MarkReminded(due.User.ID, due.Clock, day); err != nil {
			log.Printf("remind mark user=%d clock=%s: %v", due.User.ID, due.Clock, err)
			continue
		}
		if err := a.sendReminder(due); err != nil {
			log.Printf("remind send user=%d chat=%d thread=%d: %v", due.User.ID, due.ChatID, due.Thread, err)
			continue
		}
		log.Printf("remind sent user=%d chat=%d thread=%d clock=%s", due.User.ID, due.ChatID, due.Thread, due.Clock)
	}
}

func (a *App) sendReminder(due store.DueReminder) error {
	text := reminderMessage(due.User, due.Clock)
	menu := &tele.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("Отметить сегодня ✅", "chk_done")))
	args := sendArgs(due.Thread, menu)
	_, err := a.Bot.Send(tele.ChatID(due.ChatID), text, args...)
	return err
}
