package bot

import (
	"fmt"
	"html"
	"log"
	"strings"
	"unicode/utf8"

	"engbot/internal/dict"
	"engbot/internal/store"

	tele "gopkg.in/telebot.v3"
)

const maxDictMessage = 3500

func (a *App) handleWord(c tele.Context, _ store.User) error {
	q := wordQuery(c)
	if q == "" {
		msg := "Напиши слово или фразу:\n<code>/word run</code>\n<code>/word look after</code>\n\nНа русском: <code>/word привет</code>.\nВ личке можно просто текстом."
		if a.Dict == nil || !a.Dict.HasLLM() {
			msg += "\n\nСейчас нет ключа нейросети — ответы будут суше. Бесплатный ключ: console.groq.com → API Keys, в <code>.env</code> строка <code>GROQ_API_KEY=gsk_...</code>"
		}
		return a.reply(c, msg)
	}
	return a.lookupAndReply(c, q)
}

func (a *App) handleDictButton(c tele.Context, _ store.User) error {
	return a.handleWord(c, store.User{})
}

func (a *App) handlePlainText(c tele.Context) error {
	if c == nil {
		return nil
	}
	chat := c.Chat()
	if chat == nil || chat.Type != tele.ChatPrivate {
		return nil
	}
	m := c.Message()
	if m == nil {
		return nil
	}
	text := strings.TrimSpace(m.Text)
	if text == "" || strings.HasPrefix(text, "/") {
		return nil
	}
	return a.withUser(func(c tele.Context, _ store.User) error {
		return a.lookupAndReply(c, text)
	})(c)
}

func (a *App) lookupAndReply(c tele.Context, q string) error {
	if a.Dict == nil {
		return a.reply(c, "Словарь сейчас выключен.")
	}
	if c != nil {
		_ = c.Notify(tele.Typing)
	}
	log.Printf("dict lookup q=%q llm=%v", dict.NormalizeQuery(q), a.Dict.HasLLM())
	res, err := a.Dict.Lookup(q)
	if err != nil {
		log.Printf("dict lookup: %v", err)
		if strings.Contains(err.Error(), "нейросеть") {
			return a.reply(c, "Подробный разбор не успел собраться. Это не перевод-однострочник — напиши слово ещё раз.")
		}
		return a.reply(c, "Не получилось разобрать слово. Попробуй ещё раз.")
	}
	if res.Cached {
		log.Printf("dict cache hit q=%q", dict.NormalizeQuery(q))
	}
	parts := formatDictParts(res, a.Dict.HasLLM())
	for _, part := range parts {
		if err := a.reply(c, part); err != nil {
			return err
		}
	}
	return nil
}

func wordQuery(c tele.Context) string {
	if c == nil {
		return ""
	}
	if c.Callback() != nil {
		return ""
	}
	arg := strings.TrimSpace(c.Data())
	if arg != "" {
		return arg
	}
	m := c.Message()
	if m != nil && m.ReplyTo != nil {
		return strings.TrimSpace(m.ReplyTo.Text)
	}
	return ""
}

func formatDict(res dict.Result, llm bool) string {
	parts := formatDictParts(res, llm)
	return strings.Join(parts, "\n")
}

func formatDictParts(res dict.Result, llm bool) []string {
	head := formatDictHead(res)
	senses := res.Senses
	if len(senses) == 0 {
		senses = sensesFromLegacy(res.Meanings)
	}
	if len(senses) == 0 {
		var b strings.Builder
		b.WriteString(head)
		if res.Translation != "" {
			fmt.Fprintf(&b, "\n%s", html.EscapeString(res.Translation))
		} else {
			b.WriteString("\nНе нашла устойчивых значений.")
		}
		return []string{clipHTML(b.String(), maxDictMessage)}
	}

	var chunks []string
	var cur strings.Builder
	cur.WriteString(head)
	footer := ""
	if !llm && res.Source != "llm" {
		footer = "\n<i>Для полного разбора с русским контекстом нужен GROQ_API_KEY.</i>"
	}
	for i, s := range senses {
		block := formatSense(i+1, s)
		chunks = appendDictBlock(chunks, &cur, res.Word, block, footer)
	}
	extra := formatDictExtra(res)
	if extra != "" {
		chunks = appendDictBlock(chunks, &cur, res.Word, extra, footer)
	}
	if footer != "" {
		cur.WriteString(footer)
	}
	chunks = append(chunks, clipHTML(cur.String(), maxDictMessage))
	return chunks
}

func appendDictBlock(chunks []string, cur *strings.Builder, word, block, footer string) []string {
	if block == "" {
		return chunks
	}
	room := maxDictMessage - len(footer)
	if room < 200 {
		room = maxDictMessage
	}
	if cur.Len() > 0 && cur.Len()+len(block) > room {
		chunks = append(chunks, cur.String())
		cur.Reset()
		fmt.Fprintf(cur, "📖 <b>%s</b> · продолжение\n", html.EscapeString(word))
	}
	if cur.Len()+len(block) <= room {
		cur.WriteString(block)
		return chunks
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
		cur.Reset()
		fmt.Fprintf(cur, "📖 <b>%s</b> · продолжение\n", html.EscapeString(word))
	}
	remain := block
	for len(remain) > 0 {
		limit := room - cur.Len()
		if limit < 80 {
			chunks = append(chunks, cur.String())
			cur.Reset()
			fmt.Fprintf(cur, "📖 <b>%s</b> · продолжение\n", html.EscapeString(word))
			limit = room - cur.Len()
		}
		if len(remain) <= limit {
			cur.WriteString(remain)
			break
		}
		cut := splitAt(remain, limit)
		cur.WriteString(remain[:cut])
		chunks = append(chunks, cur.String())
		cur.Reset()
		fmt.Fprintf(cur, "📖 <b>%s</b> · продолжение\n", html.EscapeString(word))
		remain = remain[cut:]
	}
	return chunks
}

func splitAt(s string, max int) int {
	if max <= 0 || len(s) <= max {
		return len(s)
	}
	cut := max
	if i := strings.LastIndex(s[:max], "\n"); i > max/3 {
		cut = i + 1
	}
	return cut
}

func formatDictHead(res dict.Result) string {
	var b strings.Builder
	title := res.Word
	if title == "" {
		title = res.Normalized
	}
	fmt.Fprintf(&b, "📖 <b>%s</b>", html.EscapeString(title))
	if res.Phonetic != "" {
		fmt.Fprintf(&b, "  <i>%s</i>", html.EscapeString(res.Phonetic))
	}
	b.WriteByte('\n')
	if res.CEFR != "" {
		fmt.Fprintf(&b, "уровень <b>%s</b>", html.EscapeString(res.CEFR))
		if res.CEFRWhy != "" {
			fmt.Fprintf(&b, " — %s", html.EscapeString(res.CEFRWhy))
		}
		b.WriteByte('\n')
	}
	if res.Forms != "" {
		fmt.Fprintf(&b, "формы: <code>%s</code>\n", html.EscapeString(res.Forms))
	}
	if res.Register != "" {
		fmt.Fprintf(&b, "стиль: %s\n", html.EscapeString(res.Register))
	}
	if res.FromRussian && res.Query != "" && !strings.EqualFold(res.Query, title) {
		fmt.Fprintf(&b, "запрос: %s\n", html.EscapeString(res.Query))
	}
	if res.Overview != "" {
		fmt.Fprintf(&b, "\n<b>Суть</b>\n%s\n", html.EscapeString(res.Overview))
	}
	if res.Etymology != "" {
		fmt.Fprintf(&b, "\n<b>Откуда слово</b>\n%s\n", html.EscapeString(res.Etymology))
	}
	if res.Family != "" {
		fmt.Fprintf(&b, "семья: %s\n", html.EscapeString(res.Family))
	}
	if res.Compare != "" {
		fmt.Fprintf(&b, "\n<b>Не путать с</b>\n%s\n", html.EscapeString(res.Compare))
	}
	return b.String()
}

func formatSense(n int, s dict.Sense) string {
	var b strings.Builder
	b.WriteByte('\n')
	head := fmt.Sprintf("%d.", n)
	if s.POS != "" {
		head += " " + s.POS
	}
	fmt.Fprintf(&b, "<b>%s</b>", html.EscapeString(head))
	if s.RU != "" {
		fmt.Fprintf(&b, " — %s", html.EscapeString(s.RU))
	}
	b.WriteByte('\n')
	if s.Explain != "" {
		fmt.Fprintf(&b, "%s\n", html.EscapeString(s.Explain))
	} else if s.EN != "" {
		fmt.Fprintf(&b, "англ.: %s\n", html.EscapeString(s.EN))
	}
	if s.EN != "" && s.Explain != "" {
		fmt.Fprintf(&b, "<i>%s</i>\n", html.EscapeString(s.EN))
	}
	if s.Pattern != "" {
		fmt.Fprintf(&b, "схема: <code>%s</code>\n", html.EscapeString(s.Pattern))
	}
	if s.Grammar != "" {
		fmt.Fprintf(&b, "грамматика: %s\n", html.EscapeString(s.Grammar))
	}
	if s.Formality != "" {
		fmt.Fprintf(&b, "стиль: %s\n", html.EscapeString(s.Formality))
	}
	if s.Context != "" {
		fmt.Fprintf(&b, "когда: %s\n", html.EscapeString(s.Context))
	}
	if s.Contrast != "" {
		fmt.Fprintf(&b, "не путать: %s\n", html.EscapeString(s.Contrast))
	}
	if s.Note != "" {
		fmt.Fprintf(&b, "заметка: %s\n", html.EscapeString(s.Note))
	}
	if len(s.Synonyms) > 0 {
		fmt.Fprintf(&b, "рядом: %s\n", html.EscapeString(strings.Join(s.Synonyms, ", ")))
	}
	if len(s.Examples) > 0 {
		b.WriteString("примеры:\n")
	}
	for _, ex := range s.Examples {
		if ex.EN == "" {
			continue
		}
		fmt.Fprintf(&b, "• <i>%s</i>\n", html.EscapeString(ex.EN))
		if ex.RU != "" {
			fmt.Fprintf(&b, "  %s\n", html.EscapeString(ex.RU))
		}
	}
	return b.String()
}

func formatDictExtra(res dict.Result) string {
	var b strings.Builder
	if len(res.Collocations) > 0 {
		b.WriteString("\n<b>Часто вместе</b>\n")
		for _, c := range res.Collocations {
			fmt.Fprintf(&b, "• <code>%s</code>\n", html.EscapeString(c))
		}
	}
	if len(res.Antonyms) > 0 {
		fmt.Fprintf(&b, "\n<b>Антонимы</b>\n%s\n", html.EscapeString(strings.Join(res.Antonyms, ", ")))
	}
	if res.Mistake != "" {
		fmt.Fprintf(&b, "\n<b>Ошибка</b>\n%s\n", html.EscapeString(res.Mistake))
	}
	if res.Tip != "" {
		fmt.Fprintf(&b, "\n<b>Как запомнить</b>\n%s\n", html.EscapeString(res.Tip))
	}
	return b.String()
}

func sensesFromLegacy(ms []dict.Meaning) []dict.Sense {
	var out []dict.Sense
	for _, m := range ms {
		for _, def := range m.Definitions {
			s := dict.Sense{POS: m.PartOfSpeech, EN: def}
			for _, ex := range m.Examples {
				s.Examples = append(s.Examples, dict.Example{EN: ex})
			}
			out = append(out, s)
		}
	}
	return out
}

func clipHTML(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
