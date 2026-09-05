package bot

import (
	"path/filepath"
	"strings"
	"testing"

	"engbot/internal/store"

	tele "gopkg.in/telebot.v3"
)

func testBot(t *testing.T) *tele.Bot {
	t.Helper()
	b, err := tele.NewBot(tele.Settings{Offline: true})
	if err != nil {
		t.Fatalf("offline bot: %v", err)
	}
	return b
}

func TestThreadIDFromCommand(t *testing.T) {
	b := testBot(t)
	c := b.NewContext(tele.Update{
		Message: &tele.Message{
			ThreadID: 42,
			Text:     "/done",
			Chat:     &tele.Chat{ID: -100123, Type: tele.ChatSuperGroup},
			Sender:   &tele.User{ID: 7, FirstName: "Ann"},
		},
	})
	if got := threadID(c); got != 42 {
		t.Fatalf("command threadID=%d want 42", got)
	}
}

func TestThreadIDFromCallback(t *testing.T) {
	b := testBot(t)
	c := b.NewContext(tele.Update{
		Callback: &tele.Callback{
			ID:     "cb1",
			Sender: &tele.User{ID: 7, FirstName: "Ann"},
			Message: &tele.Message{
				ThreadID: 99,
				Chat:     &tele.Chat{ID: -100123, Type: tele.ChatSuperGroup},
			},
		},
	})
	if got := threadID(c); got != 99 {
		t.Fatalf("callback threadID=%d want 99", got)
	}
}

func TestThreadIDNilMessageAndCallback(t *testing.T) {
	b := testBot(t)
	if got := threadID(nil); got != 0 {
		t.Fatalf("nil context threadID=%d", got)
	}
	empty := b.NewContext(tele.Update{})
	if got := threadID(empty); got != 0 {
		t.Fatalf("empty update threadID=%d", got)
	}
	cb := b.NewContext(tele.Update{
		Callback: &tele.Callback{
			ID:      "gone",
			Sender:  &tele.User{ID: 7},
			Message: nil,
		},
	})
	if got := threadID(cb); got != 0 {
		t.Fatalf("nil callback message threadID=%d", got)
	}
	chatID, userID, tid := ContextInfo(cb)
	if chatID != 0 || userID != 7 || tid != 0 {
		t.Fatalf("ContextInfo=%d %d %d", chatID, userID, tid)
	}
}

func TestThreadIDSkippedInPrivateAndZero(t *testing.T) {
	b := testBot(t)

	private := b.NewContext(tele.Update{
		Message: &tele.Message{
			ThreadID: 5,
			Chat:     &tele.Chat{ID: 1, Type: tele.ChatPrivate},
		},
	})
	if got := threadID(private); got != 0 {
		t.Fatalf("private threadID=%d want 0", got)
	}

	zero := b.NewContext(tele.Update{
		Message: &tele.Message{
			ThreadID: 0,
			Chat:     &tele.Chat{ID: -100123, Type: tele.ChatSuperGroup},
		},
	})
	if got := threadID(zero); got != 0 {
		t.Fatalf("zero threadID=%d want 0", got)
	}
}

func TestEffectiveThreadIDPrefersIncoming(t *testing.T) {
	b := testBot(t)
	c := b.NewContext(tele.Update{
		Message: &tele.Message{
			ThreadID: 42,
			Chat:     &tele.Chat{ID: -100123, Type: tele.ChatSuperGroup},
		},
	})
	if got := effectiveThreadID(c, 7); got != 42 {
		t.Fatalf("got %d want incoming 42", got)
	}

	plain := b.NewContext(tele.Update{
		Message: &tele.Message{
			Chat: &tele.Chat{ID: -100123, Type: tele.ChatSuperGroup},
		},
	})
	if got := effectiveThreadID(plain, 7); got != 7 {
		t.Fatalf("got %d want stored 7", got)
	}

	priv := b.NewContext(tele.Update{
		Message: &tele.Message{
			Chat: &tele.Chat{ID: 1, Type: tele.ChatPrivate},
		},
	})
	if got := effectiveThreadID(priv, 7); got != 0 {
		t.Fatalf("private stored fallback got %d want 0", got)
	}

	if got := effectiveThreadID(nil, 7); got != 0 {
		t.Fatalf("nil context stored fallback got %d want 0", got)
	}
}

func TestSendArgsAttachesThreadID(t *testing.T) {
	menu := &tele.ReplyMarkup{}
	args := sendArgs(42, menu)
	if len(args) != 3 {
		t.Fatalf("len(args)=%d want 3", len(args))
	}
	opt, ok := args[0].(*tele.SendOptions)
	if !ok {
		t.Fatalf("first arg %T want *tele.SendOptions", args[0])
	}
	if opt.ThreadID != 42 {
		t.Fatalf("ThreadID=%d want 42", opt.ThreadID)
	}
	if args[1] != tele.ModeHTML {
		t.Fatalf("second arg %v want ModeHTML", args[1])
	}
	if args[2] != menu {
		t.Fatalf("markup not passed through")
	}
}

func TestSendArgsOmitsZeroThread(t *testing.T) {
	args := sendArgs(0)
	if len(args) != 1 {
		t.Fatalf("len(args)=%d want 1", len(args))
	}
	if _, ok := args[0].(*tele.SendOptions); ok {
		t.Fatal("must not send SendOptions when thread id is 0")
	}
	if args[0] != tele.ModeHTML {
		t.Fatalf("got %v want ModeHTML", args[0])
	}
}

func TestDisplayNameEscapesHTML(t *testing.T) {
	got := displayName(store.User{FirstName: `<b>x</b>`})
	if strings.Contains(got, "<b>") {
		t.Fatalf("raw html leaked: %s", got)
	}
	if !strings.Contains(got, "&lt;b&gt;") {
		t.Fatalf("expected escaped name, got %s", got)
	}
	got = displayName(store.User{Username: `a&b`})
	if got != "@a&amp;b" {
		t.Fatalf("username=%s", got)
	}
}

func TestReplyWithNilContext(t *testing.T) {
	if err := replyWith(nil, 0, "hi"); err == nil {
		t.Fatal("expected error")
	}
	a := &App{}
	if err := a.reply(nil, "hi"); err == nil {
		t.Fatal("expected error")
	}
}

func TestReplyNilCallbackMessageDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db.json"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	b := testBot(t)
	a := &App{Bot: b, Store: st}
	c := b.NewContext(tele.Update{
		Callback: &tele.Callback{
			ID:      "x",
			Sender:  &tele.User{ID: 1, FirstName: `<script>`},
			Message: nil,
		},
	})
	if got := threadID(c); got != 0 {
		t.Fatalf("threadID=%d", got)
	}
	if got := effectiveThreadID(c, 9); got != 0 {
		t.Fatalf("effectiveThreadID=%d", got)
	}
	chatID, userID, tid := ContextInfo(c)
	if chatID != 0 || userID != 1 || tid != 0 {
		t.Fatalf("ContextInfo=%d %d %d", chatID, userID, tid)
	}
	if a.Store.ChatTopic(chatID) != 0 {
		t.Fatal("stored topic for missing chat")
	}
}

func TestUnknownCallbackNilSafe(t *testing.T) {
	a := &App{}
	if err := a.handleUnknownCallback(nil); err != nil {
		t.Fatal(err)
	}
	b := testBot(t)
	empty := b.NewContext(tele.Update{})
	if err := a.handleUnknownCallback(empty); err != nil {
		t.Fatal(err)
	}
}

func TestMentionAndReminderMessage(t *testing.T) {
	u := store.User{ID: 42, Username: "ann", FirstName: "Ann"}
	got := mentionHTML(u)
	if !strings.Contains(got, "tg://user?id=42") || !strings.Contains(got, "@ann") {
		t.Fatalf("mention=%s", got)
	}
	msg := reminderMessage(u, "19:00")
	if !strings.Contains(msg, "@ann") || !strings.Contains(msg, "19:00") {
		t.Fatalf("msg=%s", msg)
	}
	plain := mentionHTML(store.User{ID: 7, FirstName: `<b>x</b>`})
	if strings.Contains(plain, "<b>") {
		t.Fatalf("html leak: %s", plain)
	}
}
