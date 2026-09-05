package dict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const defaultGroqURL = "https://api.groq.com/openai/v1/chat/completions"
const defaultGroqModel = "openai/gpt-oss-120b"

var groqModelFallbacks = []string{
	"openai/gpt-oss-120b",
	"openai/gpt-oss-20b",
}

type groqReq struct {
	Model          string              `json:"model"`
	Temperature    float64             `json:"temperature"`
	MaxTokens      int                 `json:"max_tokens"`
	ResponseFormat *groqRespFormat     `json:"response_format,omitempty"`
	Messages       []groqMsg           `json:"messages"`
}

type groqRespFormat struct {
	Type string `json:"type"`
}

type groqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type llmArticle struct {
	Headword     string   `json:"headword"`
	Phonetic     string   `json:"phonetic"`
	Forms        string   `json:"forms"`
	Register     string   `json:"register"`
	CEFR         string   `json:"cefr"`
	CEFRWhy      string   `json:"cefr_why"`
	Overview     string   `json:"overview"`
	Etymology    string   `json:"etymology"`
	Family       string   `json:"family"`
	Compare      string   `json:"compare"`
	Collocations []string `json:"collocations"`
	Antonyms     []string `json:"antonyms"`
	Tip          string   `json:"tip"`
	Mistake      string   `json:"mistake"`
	Senses       []struct {
		POS       string   `json:"pos"`
		RU        string   `json:"ru"`
		EN        string   `json:"en"`
		Explain   string   `json:"explain"`
		Context   string   `json:"context"`
		Pattern   string   `json:"pattern"`
		Note      string   `json:"note"`
		Contrast  string   `json:"contrast"`
		Grammar   string   `json:"grammar"`
		Formality string   `json:"formality"`
		Synonyms  []string `json:"synonyms"`
		Examples  []struct {
			EN string `json:"en"`
			RU string `json:"ru"`
		} `json:"examples"`
	} `json:"senses"`
}

func (c *Client) HasLLM() bool {
	return c != nil && strings.TrimSpace(c.GroqKey) != ""
}

func (c *Client) explain(query, dictHint string) (Result, error) {
	prompts := []string{explainPrompt(query, dictHint), compactExplainPrompt(query)}
	modes := []bool{true, true, false}
	var last error
	i := 0
	for _, jsonMode := range modes {
		prompt := prompts[0]
		if i > 0 {
			prompt = prompts[1]
		}
		i++
		content, err := c.groqChat(prompt, jsonMode)
		if err != nil {
			last = err
			continue
		}
		art, err := parseLLMArticle(content)
		if err != nil {
			last = err
			continue
		}
		out, err := resultFromArticle(query, art)
		if err != nil {
			last = err
			continue
		}
		if err := articleQuality(out, query); err != nil {
			last = err
			continue
		}
		return out, nil
	}
	if last == nil {
		last = fmt.Errorf("модель не вернула статью")
	}
	return Result{}, last
}

func resultFromArticle(query string, art llmArticle) (Result, error) {
	out := Result{
		Query:        query,
		Normalized:   query,
		Word:         strings.TrimSpace(art.Headword),
		Phonetic:     strings.TrimSpace(art.Phonetic),
		Forms:        strings.TrimSpace(art.Forms),
		Register:     strings.TrimSpace(art.Register),
		CEFR:         NormalizeCEFR(art.CEFR),
		CEFRWhy:      strings.TrimSpace(art.CEFRWhy),
		Collocations: cleanList(art.Collocations, 12),
		Antonyms:     cleanList(art.Antonyms, 8),
		Tip:          strings.TrimSpace(art.Tip),
		Mistake:      strings.TrimSpace(art.Mistake),
		Overview:     strings.TrimSpace(art.Overview),
		Etymology:    strings.TrimSpace(art.Etymology),
		Family:       strings.TrimSpace(art.Family),
		Compare:      strings.TrimSpace(art.Compare),
		Source:       "llm",
	}
	if out.Word == "" {
		out.Word = query
	}
	for _, s := range art.Senses {
		if strings.TrimSpace(s.RU) == "" && strings.TrimSpace(s.EN) == "" {
			continue
		}
		sense := Sense{
			POS:       strings.TrimSpace(s.POS),
			RU:        strings.TrimSpace(s.RU),
			EN:        strings.TrimSpace(s.EN),
			Explain:   strings.TrimSpace(s.Explain),
			Context:   strings.TrimSpace(s.Context),
			Pattern:   strings.TrimSpace(s.Pattern),
			Note:      strings.TrimSpace(s.Note),
			Contrast:  strings.TrimSpace(s.Contrast),
			Grammar:   strings.TrimSpace(s.Grammar),
			Formality: strings.TrimSpace(s.Formality),
			Synonyms:  cleanList(s.Synonyms, 6),
		}
		for _, ex := range s.Examples {
			en := strings.TrimSpace(ex.EN)
			ru := strings.TrimSpace(ex.RU)
			if en == "" {
				continue
			}
			sense.Examples = append(sense.Examples, Example{EN: en, RU: ru})
			if len(sense.Examples) >= 5 {
				break
			}
		}
		out.Senses = append(out.Senses, sense)
		if len(out.Senses) >= 8 {
			break
		}
	}
	if len(out.Senses) == 0 {
		return Result{}, fmt.Errorf("модель не вернула значения")
	}
	return out, nil
}

func articleQuality(res Result, query string) error {
	if len(res.Senses) == 0 {
		return fmt.Errorf("нет значений")
	}
	if !headwordMatchesQuery(res.Word, query) {
		return fmt.Errorf("статья не про %q", query)
	}
	rich := utf8.RuneCountInString(res.Overview) >= 40
	examples := 0
	for _, s := range res.Senses {
		if utf8.RuneCountInString(s.Explain) >= 40 {
			rich = true
		}
		examples += len(s.Examples)
	}
	if !rich {
		return fmt.Errorf("слишком коротко")
	}
	if examples == 0 {
		return fmt.Errorf("нет примеров")
	}
	return nil
}

func headwordMatchesQuery(word, query string) bool {
	q := strings.ToLower(NormalizeQuery(query))
	w := strings.ToLower(NormalizeQuery(word))
	if q == "" || w == "" || !strings.Contains(q, " ") {
		return true
	}
	wf := strings.Fields(w)
	if len(wf) < 2 {
		return false
	}
	return strings.Contains(w, q) || strings.Contains(q, w)
}

func explainPrompt(query, dictHint string) string {
	var b strings.Builder
	b.WriteString("Ты лексикограф для русскоязычных. Нужна учебная статья, не машинный перевод одной строкой.\n")
	b.WriteString("Запрос: ")
	b.WriteString(query)
	b.WriteString("\n")
	if strings.Contains(strings.TrimSpace(query), " ") {
		b.WriteString("Это ФРАЗА / фразовый глагол. Разбери её ЦЕЛИКОМ. Не подменяй на первое слово. Сравни с близкими фразами.\n")
	}
	if strings.TrimSpace(dictHint) != "" {
		b.WriteString("Справка:\n")
		b.WriteString(dictHint)
		b.WriteString("\n")
	}
	b.WriteString(`Верни ТОЛЬКО JSON:
{"headword":"лемма как в запросе","phonetic":"/ipa/","forms":"look after / looks after / looking after / looked after","register":"нейтрально/разговорное/официально","cefr":"A2","cefr_why":"почему такой уровень, 1–2 предложения","overview":"4–6 предложений: что это за единица, главные смыслы, как её чувствует носитель","etymology":"если помогает","family":"однокоренные и близкие фразы","compare":"look after vs look for vs look at vs take care of vs watch — по фразе","collocations":["look after children","look after yourself"],"antonyms":["neglect"],"tip":"как запомнить","mistake":"ошибки русскоязычных, не look after = look","senses":[{"pos":"фразовый глагол","ru":"несколько русских эквивалентов этого смысла","en":"gloss","explain":"3–5 предложений: образ, кто за кем, ответственность, не «просто смотреть»","context":"семья, работа, здоровье","pattern":"look after sb/sth","note":"","contrast":"не look at и не babysit в узком смысле, если отличается","grammar":"не разделяется: look the child after нельзя","formality":"нейтрально","synonyms":["take care of"],"examples":[{"en":"...","ru":"..."}]}]}
Правила:
- 2–5 живых значений по частоте, не одно слово-перевод;
- у каждого: ru, explain (≥3 предложения), context, contrast, 3 примера EN+RU (быт, забота о человеке, чуть формальнее);
- JSON без markdown.`)
	return b.String()
}

func compactExplainPrompt(query string) string {
	return "Запрос: " + query + "\nЭто должна быть учебная статья JSON, не перевод. Если фраза — разбери всю фразу, headword не одно слово.\n" +
		`{"headword":"look after","overview":"4 предложения","cefr":"A2","cefr_why":"...","compare":"vs look for / take care of","mistake":"...","tip":"...","senses":[{"pos":"phrasal verb","ru":"присматривать; заботиться; ухаживать","explain":"3 предложения почему это ответственность, а не взгляд","context":"...","contrast":"не look at","pattern":"look after sb","examples":[{"en":"...","ru":"..."},{"en":"...","ru":"..."},{"en":"...","ru":"..."}]}]}`
}

func cleanList(items []string, max int) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}

func (c *Client) groqChat(prompt string, jsonMode bool) (string, error) {
	if c == nil || strings.TrimSpace(c.GroqKey) == "" {
		return "", fmt.Errorf("нет Groq-ключа")
	}
	if c.HTTP == nil {
		return "", fmt.Errorf("нет HTTP-клиента")
	}
	var last error
	for _, model := range c.modelsToTry() {
		content, err := c.groqChatModel(model, prompt, jsonMode)
		if err == nil {
			c.rememberGroqModel(model)
			return content, nil
		}
		last = err
		if !unknownGroqModel(err) {
			return "", err
		}
		log.Printf("dict groq model %s недоступна, пробую другую", model)
	}
	if last == nil {
		last = fmt.Errorf("нет доступной модели Groq")
	}
	return "", last
}

func (c *Client) groqChatModel(model, prompt string, jsonMode bool) (string, error) {
	body := groqReq{
		Model:       model,
		Temperature: 0.2,
		MaxTokens:   8000,
		Messages: []groqMsg{
			{Role: "system", Content: "Отвечай только валидным JSON."},
			{Role: "user", Content: prompt},
		},
	}
	if jsonMode {
		body.ResponseFormat = &groqRespFormat{Type: "json_object"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.groqURL(), bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.GroqKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.groqHTTP().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var parsed groqResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("groq: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("groq: HTTP %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("groq: пустой ответ")
	}
	return parsed.Choices[0].Message.Content, nil
}

func (c *Client) modelsToTry() []string {
	c.cacheMu.Lock()
	resolved := c.groqResolved
	c.cacheMu.Unlock()
	if resolved != "" {
		return []string{resolved}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = canonicalGroqModel(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(c.GroqModel)
	add(defaultGroqModel)
	for _, m := range groqModelFallbacks {
		add(m)
	}
	return out
}

func canonicalGroqModel(m string) string {
	switch strings.TrimSpace(m) {
	case "", "llama-3.3-70b-versatile", "llama-3.1-70b-versatile", "llama-3.1-8b-instant":
		return defaultGroqModel
	default:
		return strings.TrimSpace(m)
	}
}

func unknownGroqModel(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "does not exist") ||
		strings.Contains(s, "do not have access") ||
		strings.Contains(s, "model_decommissioned") ||
		strings.Contains(s, "decommissioned") ||
		strings.Contains(s, "model_not_found")
}

func (c *Client) rememberGroqModel(model string) {
	if c == nil || model == "" {
		return
	}
	c.cacheMu.Lock()
	if c.groqResolved == "" {
		c.groqResolved = model
		log.Printf("dict groq model=%s", model)
	}
	c.cacheMu.Unlock()
}

func (c *Client) groqHTTP() *http.Client {
	base := c.HTTP
	if base == nil {
		return &http.Client{Timeout: 120 * time.Second}
	}
	if base.Timeout == 0 || base.Timeout >= 90*time.Second {
		return base
	}
	return &http.Client{Timeout: 120 * time.Second, Transport: base.Transport}
}

func (c *Client) groqURL() string {
	if c != nil && c.GroqURL != "" {
		return c.GroqURL
	}
	return defaultGroqURL
}

func parseLLMArticle(content string) (llmArticle, error) {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var art llmArticle
	if err := json.Unmarshal([]byte(s), &art); err != nil {
		return llmArticle{}, err
	}
	return art, nil
}

func NormalizeCEFR(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	allowed := map[string]string{
		"A1": "A1", "A2": "A2", "B1": "B1", "B2": "B2", "C1": "C1", "C2": "C2",
		"A1-A2": "A1–A2", "A2-B1": "A2–B1", "B1-B2": "B1–B2", "B2-C1": "B2–C1", "C1-C2": "C1–C2",
	}
	if v, ok := allowed[s]; ok {
		return v
	}
	var buf strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		}
		if buf.Len() >= 2 {
			break
		}
	}
	code := buf.String()
	if v, ok := allowed[code]; ok {
		return v
	}
	return ""
}
