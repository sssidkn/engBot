package bot

import (
	"fmt"
	"html"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"engbot/internal/store"

	tele "gopkg.in/telebot.v3"
)

type App struct {
	Bot   *tele.Bot
	Store *store.Store
}

func New(b *tele.Bot, s *store.Store) *App {
	a := &App{Bot: b, Store: s}
	if b != nil {
		b.Use(func(next tele.HandlerFunc) tele.HandlerFunc {
			return func(c tele.Context) error {
				logUpdate(c)
				if err := next(c); err != nil {
					chatID, userID, tid := ContextInfo(c)
					log.Printf("handler error chat=%d user=%d thread=%d: %v", chatID, userID, tid, err)
					return err
				}
				return nil
			}
		})
	}
	a.register()
	return a
}

func (a *App) register() {
	menu := mainMenu()

	a.Bot.Handle("/start", a.withUser(func(c tele.Context, _ store.User) error {
		return a.reply(c, menuText(), menu)
	}))
	a.Bot.Handle("/help", a.withUser(func(c tele.Context, _ store.User) error {
		return a.reply(c, commandsHelp(), menu)
	}))
	a.Bot.Handle("/done", a.withUser(a.handleDone))
	a.Bot.Handle("/undo", a.withUser(a.handleUndo))
	a.Bot.Handle("/streak", a.withUser(a.handleStreak))
	a.Bot.Handle("/calendar", a.withUser(a.handleCalendar))
	a.Bot.Handle("/stats", a.withUser(a.handleStats))
	a.Bot.Handle("/board", a.withUser(a.handleBoard))
	a.Bot.Handle("/timezone", a.withUser(a.handleTimezone))
	a.Bot.Handle("/remind", a.withUser(a.handleRemind))
	a.Bot.Handle("/here", a.withUser(a.handleHere))
	a.Bot.Handle(tele.OnAddedToGroup, a.handleAddedToGroup)

	a.Bot.Handle(&tele.Btn{Unique: "chk_done"}, a.withUser(a.handleDone))
	a.Bot.Handle(&tele.Btn{Unique: "chk_cal"}, a.withUser(a.handleCalendar))
	a.Bot.Handle(&tele.Btn{Unique: "chk_streak"}, a.withUser(a.handleStreak))
	a.Bot.Handle(&tele.Btn{Unique: "chk_stats"}, a.withUser(a.handleStats))
	a.Bot.Handle(&tele.Btn{Unique: "chk_remind"}, a.withUser(a.handleRemindButton))
	a.Bot.Handle(&tele.Btn{Unique: calNavUnique}, a.withUser(a.handleCalNav))
	a.Bot.Handle(&tele.Btn{Unique: calDayUnique}, a.withUser(a.handleCalDay))
	a.Bot.Handle(&tele.Btn{Unique: calNoopUnique}, a.handleCalNoop)
	a.Bot.Handle(&tele.Btn{Unique: calMenuUnique}, a.withUser(a.handleCalMenu))
	a.Bot.Handle(&tele.Btn{Unique: rmdDelUnique}, a.withUser(a.handleRemindDel))
	a.Bot.Handle(&tele.Btn{Unique: rmdClrUnique}, a.withUser(a.handleRemindClear))
	a.Bot.Handle(tele.OnCallback, a.handleUnknownCallback)
}

func commandsHelp() string {
	return strings.Join([]string{
		"<b>Команды</b>",
		"/done — отметить занятие сегодня",
		"/undo — убрать отметку за сегодня",
		"/streak — текущая серия подряд",
		"/calendar — календарь: нажми день, чтобы отметить или снять; ← Меню — назад",
		"/stats — сколько дней всего и лучшая серия",
		"/board — кто как занимается в этом чате",
		"/timezone Europe/Moscow — свой часовой пояс (от него зависит «сегодня» и напоминания)",
		"/remind 09:00 — добавить напоминание (можно несколько времён)",
		"/remind — список; /remind off — выключить все",
		"/here — отвечать в этой теме форума",
		"",
		"В чате с темами пиши команды внутри нужной темы — бот отвечает туда же.",
		"Чтобы бот видел обычные сообщения, в BotFather выключите Group Privacy.",
	}, "\n")
}

func menuText() string {
	return "Привет! Это бот для занятий английским.\n\n" +
		"Отмечай дни занятий в календаре: любой прошедший день можно поставить или снять. Посчитаю серию подряд и покажу пропуски.\n\n" +
		"Можно писать в личку или добавить бота в общий чат: у каждого своя история, в группе есть таблица /board.\n\n" +
		commandsHelp()
}

func mainMenu() *tele.ReplyMarkup {
	menu := &tele.ReplyMarkup{}
	btnDone := menu.Data("Отметить сегодня ✅", "chk_done")
	btnCal := menu.Data("Календарь 📅", "chk_cal")
	btnStreak := menu.Data("Серия 🔥", "chk_streak")
	btnStats := menu.Data("Статистика 📊", "chk_stats")
	btnRemind := menu.Data("Напоминания ⏰", "chk_remind")
	menu.Inline(
		menu.Row(btnDone),
		menu.Row(btnCal, btnStreak, btnStats),
		menu.Row(btnRemind),
	)
	return menu
}

func (a *App) withUser(fn func(tele.Context, store.User) error) func(tele.Context) error {
	return func(c tele.Context) error {
		if c == nil {
			return nil
		}
		u := c.Sender()
		if u == nil {
			return nil
		}
		if err := a.Store.UpsertUser(u.ID, u.Username, u.FirstName); err != nil {
			log.Printf("upsert user=%d: %v", u.ID, err)
			return a.reply(c, "Не получилось сохранить профиль. Попробуй ещё раз.")
		}
		if chat := c.Chat(); chat != nil {
			if err := a.Store.TouchChat(chat.ID, u.ID); err != nil {
				log.Printf("touch chat=%d user=%d: %v", chat.ID, u.ID, err)
			}
			if tid := threadID(c); tid != 0 {
				if err := a.Store.SetChatTopic(chat.ID, tid); err != nil {
					log.Printf("set topic chat=%d thread=%d: %v", chat.ID, tid, err)
				}
			}
		}
		user, err := a.Store.GetUser(u.ID)
		if err != nil {
			log.Printf("get user=%d: %v", u.ID, err)
			return a.reply(c, "Ошибка базы. Попробуй ещё раз.")
		}
		return fn(c, user)
	}
}

func (a *App) handleDone(c tele.Context, u store.User) error {
	added, err := a.Store.MarkToday(u.ID)
	if err != nil {
		log.Printf("mark user=%d: %v", u.ID, err)
		return a.reply(c, "Не получилось отметить день.")
	}
	log.Printf("mark user=%d added=%v", u.ID, added)
	st, err := a.Store.Stats(u.ID)
	if err != nil {
		log.Printf("stats after mark user=%d: %v", u.ID, err)
		if added {
			return a.reply(c, "Занятие засчитано.")
		}
		return a.reply(c, "Сегодня уже отмечено ✅")
	}
	name := displayName(u)
	if !added {
		return a.reply(c, fmt.Sprintf("%s, сегодня уже отмечено ✅\nСерия: <b>%d</b> %s.",
			name, st.CurrentStreak, dayWord(st.CurrentStreak)))
	}
	fire := streakFire(st.CurrentStreak)
	return a.reply(c, fmt.Sprintf("%s: занятие засчитано %s\nСерия: <b>%d</b> %s подряд. Всего дней: %d.",
		name, fire, st.CurrentStreak, dayWord(st.CurrentStreak), st.TotalDays))
}

func (a *App) handleUndo(c tele.Context, u store.User) error {
	removed, err := a.Store.UnmarkToday(u.ID)
	if err != nil {
		log.Printf("unmark user=%d: %v", u.ID, err)
		return a.reply(c, "Не получилось снять отметку.")
	}
	log.Printf("unmark user=%d removed=%v", u.ID, removed)
	if !removed {
		return a.reply(c, "Сегодня ещё не отмечено — снимать нечего.")
	}
	st, err := a.Store.Stats(u.ID)
	if err != nil {
		log.Printf("stats after undo user=%d: %v", u.ID, err)
		return a.reply(c, "Отметка за сегодня снята.")
	}
	return a.reply(c, fmt.Sprintf("Отметка за сегодня снята. Серия сейчас: <b>%d</b>.", st.CurrentStreak))
}

func (a *App) handleStreak(c tele.Context, u store.User) error {
	st, err := a.Store.Stats(u.ID)
	if err != nil {
		log.Printf("stats user=%d: %v", u.ID, err)
		return a.reply(c, "Не получилось посчитать серию.")
	}
	today := a.Store.Today(u)
	done, err := a.Store.HasDay(u.ID, today)
	if err != nil {
		log.Printf("has day user=%d: %v", u.ID, err)
	}
	status := "сегодня ещё не отмечено"
	if done {
		status = "сегодня уже есть ✅"
	}
	return a.reply(c, fmt.Sprintf(
		"<b>%s</b>\nТекущая серия: <b>%d</b> %s\nЛучшая серия: <b>%d</b>\n%s",
		displayName(u), st.CurrentStreak, dayWord(st.CurrentStreak), st.BestStreak, status,
	))
}

func (a *App) handleStats(c tele.Context, u store.User) error {
	st, err := a.Store.Stats(u.ID)
	if err != nil {
		log.Printf("stats user=%d: %v", u.ID, err)
		return a.reply(c, "Не получилось посчитать статистику.")
	}
	if st.TotalDays == 0 {
		return a.reply(c, "Пока нет отметок. Нажми /done после занятия.")
	}
	return a.reply(c, fmt.Sprintf(
		"<b>%s</b>\nДней с занятием: <b>%d</b>\nВ этом месяце: <b>%d</b>\nСерия сейчас: <b>%d</b>\nЛучшая серия: <b>%d</b>\nПервый день: %s\nПоследний день: %s",
		displayName(u), st.TotalDays, st.ThisMonth, st.CurrentStreak, st.BestStreak,
		formatRU(st.FirstDay), formatRU(st.LastDay),
	))
}

func (a *App) handleBoard(c tele.Context, _ store.User) error {
	chat := c.Chat()
	if chat == nil {
		return a.reply(c, "Открой /board в чате.")
	}
	rows, err := a.Store.ChatBoard(chat.ID)
	if err != nil {
		log.Printf("board chat=%d: %v", chat.ID, err)
		return a.reply(c, "Не получилось собрать таблицу.")
	}
	if len(rows) == 0 {
		return a.reply(c, "В этом чате ещё никто не отмечался. Напиши /done после занятия.")
	}
	var b strings.Builder
	if chat.Type == tele.ChatPrivate {
		b.WriteString("<b>Твой прогресс</b>\n")
	} else {
		b.WriteString("<b>Занятия в этом чате</b>\n")
	}
	for i, row := range rows {
		mark := "—"
		if row.DoneToday {
			mark = "✅"
		}
		fmt.Fprintf(&b, "%d. %s %s серия %d · всего %d\n",
			i+1, mark, displayName(row.User), row.CurrentStreak, row.TotalDays)
	}
	b.WriteString("\n✅ — уже есть отметка сегодня")
	return a.reply(c, b.String())
}

func (a *App) handleTimezone(c tele.Context, u store.User) error {
	arg := ""
	if c != nil && c.Callback() == nil {
		arg = strings.TrimSpace(c.Data())
	}
	if arg == "" {
		return a.reply(c, fmt.Sprintf(
			"Сейчас пояс: <code>%s</code>\n«Сегодня» для тебя: <b>%s</b>\n\nСменить: <code>/timezone Europe/Moscow</code>\nПримеры: Europe/Moscow, Europe/Kyiv, Asia/Yekaterinburg, UTC",
			html.EscapeString(u.Timezone), formatRU(a.Store.Today(u)),
		))
	}
	if err := a.Store.SetTimezone(u.ID, arg); err != nil {
		log.Printf("timezone user=%d arg=%q: %v", u.ID, arg, err)
		return a.reply(c, "Не знаю такой пояс. Пример: /timezone Europe/Moscow")
	}
	u.Timezone = arg
	log.Printf("timezone user=%d tz=%s", u.ID, arg)
	return a.reply(c, fmt.Sprintf("Пояс сохранён: <code>%s</code>. Сегодня: <b>%s</b>.",
		html.EscapeString(arg), formatRU(a.Store.Today(u))))
}

func (a *App) handleHere(c tele.Context, _ store.User) error {
	chat := c.Chat()
	if chat == nil || chat.Type == tele.ChatPrivate {
		return a.reply(c, "Команда /here нужна в групповом чате с темами.")
	}
	tid := threadID(c)
	if tid == 0 {
		return a.reply(c, "Не вижу тему. Напиши /here внутри нужной темы форума.")
	}
	if err := a.Store.SetChatTopic(chat.ID, tid); err != nil {
		log.Printf("here chat=%d thread=%d: %v", chat.ID, tid, err)
		return a.reply(c, "Не получилось запомнить тему.")
	}
	log.Printf("here chat=%d thread=%d", chat.ID, tid)
	return a.reply(c, "Буду отвечать в этой теме.")
}

func (a *App) handleAddedToGroup(c tele.Context) error {
	if c == nil {
		return nil
	}
	if chat := c.Chat(); chat != nil {
		if tid := threadID(c); tid != 0 {
			if err := a.Store.SetChatTopic(chat.ID, tid); err != nil {
				log.Printf("added-to-group topic chat=%d thread=%d: %v", chat.ID, tid, err)
			}
		}
	}
	text := "Привет! Это бот для занятий английским.\n\n" +
		"Отмечай день командой /done — посчитаю серию и покажу календарь.\n" +
		"В чате с темами пиши команды внутри нужной темы — ответ останется там.\n\n" +
		commandsHelp()
	return a.reply(c, text)
}

func (a *App) handleUnknownCallback(c tele.Context) error {
	if c == nil || c.Callback() == nil {
		return nil
	}
	if err := c.Respond(); err != nil {
		log.Printf("unknown callback respond: %v", err)
		return err
	}
	return nil
}

// threadID reads message_thread_id from the incoming update.
// Commands use Message.ThreadID; callback buttons use the same field
// on the original message (telebot exposes it via Context.Message).
// Private chats and a missing/zero thread never produce an id.
func threadID(c tele.Context) int {
	if c == nil {
		return 0
	}
	chat := c.Chat()
	if chat == nil || chat.Type == tele.ChatPrivate {
		return 0
	}
	msg := c.Message()
	if msg == nil || msg.ThreadID == 0 {
		return 0
	}
	return msg.ThreadID
}

func effectiveThreadID(c tele.Context, stored int) int {
	if tid := threadID(c); tid != 0 {
		return tid
	}
	if stored == 0 || c == nil {
		return 0
	}
	chat := c.Chat()
	if chat == nil || chat.Type == tele.ChatPrivate {
		return 0
	}
	return stored
}

// ContextInfo is safe for logging: chat, user and forum thread ids only.
func ContextInfo(c tele.Context) (chatID, userID int64, thread int) {
	if c == nil {
		return 0, 0, 0
	}
	if chat := c.Chat(); chat != nil {
		chatID = chat.ID
	}
	if u := c.Sender(); u != nil {
		userID = u.ID
	}
	return chatID, userID, threadID(c)
}

func logUpdate(c tele.Context) {
	if c == nil {
		return
	}
	chatID, userID, tid := ContextInfo(c)
	if cb := c.Callback(); cb != nil {
		log.Printf("вход callback chat=%d user=%d thread=%d unique=%s", chatID, userID, tid, cb.Unique)
		return
	}
	cmd := ""
	if m := c.Message(); m != nil {
		cmd = firstToken(m.Text)
	}
	log.Printf("вход command chat=%d user=%d thread=%d text=%s", chatID, userID, tid, cmd)
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// sendArgs always puts SendOptions first so ThreadID is not wiped by later
// ParseMode / ReplyMarkup flags. telebot maps SendOptions.ThreadID to
// message_thread_id and omits the field when ThreadID is 0.
func sendArgs(threadID int, extra ...interface{}) []interface{} {
	args := make([]interface{}, 0, 2+len(extra))
	if threadID != 0 {
		args = append(args, &tele.SendOptions{ThreadID: threadID})
	}
	args = append(args, tele.ModeHTML)
	return append(args, extra...)
}

func replyWith(c tele.Context, tid int, text string, extra ...interface{}) error {
	if c == nil {
		log.Print("send failed: nil context")
		return fmt.Errorf("nil context")
	}
	if c.Callback() != nil {
		if err := c.Respond(); err != nil {
			log.Printf("callback respond: %v", err)
		}
	}
	if err := c.Send(text, sendArgs(tid, extra...)...); err != nil {
		chatID, userID, _ := ContextInfo(c)
		log.Printf("send failed chat=%d user=%d thread=%d: %v", chatID, userID, tid, err)
		return err
	}
	return nil
}

func reply(c tele.Context, text string, extra ...interface{}) error {
	return replyWith(c, threadID(c), text, extra...)
}

func (a *App) reply(c tele.Context, text string, extra ...interface{}) error {
	if c == nil {
		log.Print("send failed: nil context")
		return fmt.Errorf("nil context")
	}
	stored := 0
	if a != nil && a.Store != nil {
		if chat := c.Chat(); chat != nil {
			stored = a.Store.ChatTopic(chat.ID)
		}
	}
	return replyWith(c, effectiveThreadID(c, stored), text, extra...)
}

func displayName(u store.User) string {
	if name := strings.TrimSpace(u.FirstName); name != "" {
		return html.EscapeString(name)
	}
	if u.Username != "" {
		return "@" + html.EscapeString(u.Username)
	}
	return "Ты"
}

func dayWord(n int) string {
	n = n % 100
	if n >= 11 && n <= 14 {
		return "дней"
	}
	switch n % 10 {
	case 1:
		return "день"
	case 2, 3, 4:
		return "дня"
	default:
		return "дней"
	}
}

func streakFire(n int) string {
	switch {
	case n >= 30:
		return "🔥🔥🔥"
	case n >= 7:
		return "🔥🔥"
	default:
		return "🔥"
	}
}

func formatRU(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return html.EscapeString(day)
	}
	return t.Format("02.01.2006")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[size:]
}
