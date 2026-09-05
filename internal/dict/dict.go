package dict

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxQueryRunes = 120
	userAgent     = "engbot/1.0 (telegram english study bot)"
	dictURL       = "https://api.dictionaryapi.dev/api/v2/entries/en/"
	translateURL  = "https://api.mymemory.translated.net/get"
)

type Client struct {
	HTTP      *http.Client
	DictBase  string
	TransBase string
	GroqKey   string
	GroqModel string
	GroqURL   string
	CachePath string

	groqResolved string

	cacheMu   sync.Mutex
	cacheOnce sync.Once
	cache     map[string]cacheEntry
}

type Example struct {
	EN string
	RU string
}

type Sense struct {
	POS        string
	RU         string
	EN         string
	Explain    string
	Context    string
	Pattern    string
	Note       string
	Contrast   string
	Grammar    string
	Formality  string
	Synonyms   []string
	Examples   []Example
}

type Meaning struct {
	PartOfSpeech string
	Definitions  []string
	Examples     []string
	Synonyms     []string
}

type Result struct {
	Query        string
	Normalized   string
	Translation  string
	Word         string
	Phonetic     string
	Forms        string
	Register     string
	Collocations []string
	Tip          string
	Mistake      string
	Overview     string
	Etymology    string
	Family       string
	Compare      string
	Antonyms     []string
	Meanings     []Meaning
	Senses       []Sense
	CEFR         string
	CEFRWhy      string
	FromRussian  bool
	Source       string
	Cached       bool
}

func New(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{HTTP: hc}
}

func (c *Client) dictBase() string {
	if c != nil && c.DictBase != "" {
		return c.DictBase
	}
	return dictURL
}

func (c *Client) transBase() string {
	if c != nil && c.TransBase != "" {
		return c.TransBase
	}
	return translateURL
}

func NormalizeQuery(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'«»“”`)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxQueryRunes {
		r := []rune(s)
		s = string(r[:maxQueryRunes])
	}
	return s
}

func LooksRussian(s string) bool {
	cyr, lat := 0, 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Cyrillic):
			cyr++
		case unicode.In(r, unicode.Latin):
			lat++
		}
	}
	return cyr > 0 && cyr >= lat
}

func (c *Client) Lookup(query string) (Result, error) {
	q := NormalizeQuery(query)
	if q == "" {
		return Result{}, fmt.Errorf("пустое слово")
	}
	if cached, ok := c.cacheGet(q); ok {
		return cached, nil
	}

	out := Result{Query: q, Normalized: q, Source: "dict"}
	lookup := q
	if LooksRussian(q) {
		out.FromRussian = true
		if !c.HasLLM() {
			if en, err := c.translate(q, "ru|en"); err == nil && strings.TrimSpace(en) != "" {
				out.Translation = en
				out.Normalized = NormalizeQuery(en)
				lookup = out.Normalized
			}
		}
	}

	if c.HasLLM() {
		llmRes, err := c.explain(lookup, "")
		if err != nil {
			log.Printf("dict llm q=%q: %v", lookup, err)
			return Result{}, fmt.Errorf("нейросеть не собрала статью")
		}
		llmRes.Query = q
		llmRes.Normalized = lookup
		llmRes.FromRussian = out.FromRussian
		c.cachePut(q, llmRes)
		return llmRes, nil
	}

	entry, dictErr := c.define(lookup)
	if dictErr != nil {
		if tok := firstToken(lookup); tok != "" && !strings.EqualFold(tok, lookup) {
			entry, dictErr = c.define(tok)
		}
	}
	if dictErr == nil {
		out.Word = entry.Word
		out.Phonetic = entry.Phonetic
		out.Meanings = entry.Meanings
	}

	out.Senses = sensesFromMeanings(out.Meanings)
	head := out.Word
	if head == "" {
		head = lookup
	}
	if out.CEFR == "" {
		out.CEFR, out.CEFRWhy = EstimateCEFR(strings.ToLower(head))
	}
	if !out.FromRussian && out.Translation == "" {
		if ru, err := c.translate(lookup, "en|ru"); err == nil {
			out.Translation = ru
		}
	}
	if dictErr != nil && len(out.Senses) == 0 && out.Translation == "" {
		return out, dictErr
	}
	c.cachePut(q, out)
	return out, nil
}

func sensesFromMeanings(ms []Meaning) []Sense {
	var out []Sense
	n := 0
	for _, m := range ms {
		for _, def := range m.Definitions {
			s := Sense{POS: m.PartOfSpeech, EN: def}
			for _, ex := range m.Examples {
				s.Examples = append(s.Examples, Example{EN: ex})
			}
			s.Synonyms = append([]string(nil), m.Synonyms...)
			out = append(out, s)
			n++
			if n >= 8 {
				return out
			}
		}
	}
	return out
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " ,.;:!?"); i > 0 {
		return s[:i]
	}
	return s
}

type dictEntry struct {
	Word     string
	Phonetic string
	Meanings []Meaning
}

type dictAPIEntry struct {
	Word      string `json:"word"`
	Phonetic  string `json:"phonetic"`
	Phonetics []struct {
		Text string `json:"text"`
	} `json:"phonetics"`
	Meanings []struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string   `json:"definition"`
			Example    string   `json:"example"`
			Synonyms   []string `json:"synonyms"`
		} `json:"definitions"`
		Synonyms []string `json:"synonyms"`
	} `json:"meanings"`
}

func (c *Client) define(word string) (dictEntry, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return dictEntry{}, fmt.Errorf("пустое слово")
	}
	req, err := http.NewRequest(http.MethodGet, c.dictBase()+url.PathEscape(word), nil)
	if err != nil {
		return dictEntry{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return dictEntry{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return dictEntry{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return dictEntry{}, fmt.Errorf("словарь: HTTP %d", resp.StatusCode)
	}
	var raw []dictAPIEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return dictEntry{}, err
	}
	if len(raw) == 0 {
		return dictEntry{}, fmt.Errorf("нет статьи")
	}
	merged := parseDictEntry(raw[0])
	for i := 1; i < len(raw); i++ {
		part := parseDictEntry(raw[i])
		merged.Meanings = append(merged.Meanings, part.Meanings...)
	}
	if len(merged.Meanings) > 10 {
		merged.Meanings = merged.Meanings[:10]
	}
	return merged, nil
}

func parseDictEntry(raw dictAPIEntry) dictEntry {
	e := dictEntry{Word: raw.Word, Phonetic: strings.TrimSpace(raw.Phonetic)}
	if e.Phonetic == "" {
		for _, p := range raw.Phonetics {
			if strings.TrimSpace(p.Text) != "" {
				e.Phonetic = strings.TrimSpace(p.Text)
				break
			}
		}
	}
	for _, m := range raw.Meanings {
		mm := Meaning{PartOfSpeech: m.PartOfSpeech}
		seenSyn := map[string]struct{}{}
		for _, s := range m.Synonyms {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seenSyn[s]; ok {
				continue
			}
			seenSyn[s] = struct{}{}
			mm.Synonyms = append(mm.Synonyms, s)
			if len(mm.Synonyms) >= 8 {
				break
			}
		}
		for _, d := range m.Definitions {
			if len(mm.Definitions) < 6 && strings.TrimSpace(d.Definition) != "" {
				mm.Definitions = append(mm.Definitions, strings.TrimSpace(d.Definition))
			}
			if len(mm.Examples) < 4 && strings.TrimSpace(d.Example) != "" {
				mm.Examples = append(mm.Examples, strings.TrimSpace(d.Example))
			}
			for _, s := range d.Synonyms {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, ok := seenSyn[s]; ok {
					continue
				}
				if len(mm.Synonyms) >= 5 {
					break
				}
				seenSyn[s] = struct{}{}
				mm.Synonyms = append(mm.Synonyms, s)
			}
		}
		if len(mm.Definitions) == 0 {
			continue
		}
		e.Meanings = append(e.Meanings, mm)
	}
	return e
}

type memoryResp struct {
	ResponseStatus int `json:"responseStatus"`
	ResponseData   struct {
		TranslatedText string `json:"translatedText"`
	} `json:"responseData"`
}

func (c *Client) translate(text, pair string) (string, error) {
	u, err := url.Parse(c.transBase())
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", text)
	q.Set("langpair", pair)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("перевод: HTTP %d", resp.StatusCode)
	}
	var parsed memoryResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	tr := strings.TrimSpace(parsed.ResponseData.TranslatedText)
	if parsed.ResponseStatus != 200 || tr == "" {
		return "", fmt.Errorf("перевод недоступен")
	}
	return tr, nil
}
