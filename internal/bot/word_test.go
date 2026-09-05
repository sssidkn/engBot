package bot

import (
	"strings"
	"testing"

	"engbot/internal/dict"
)

func TestFormatDictEscapesAndIncludesSenses(t *testing.T) {
	got := formatDict(dict.Result{
		Word:    "hello",
		Phonetic: "/həˈloʊ/",
		CEFR:    "A1",
		CEFRWhy:   "базовое приветствие",
		Overview:  "короткое приветствие",
		Etymology: "из староанглийского",
		Family:    "hello, greeting",
		Compare:   "hi короче",
		Antonyms:  []string{"goodbye"},
		Senses: []dict.Sense{{
			POS:       "noun",
			RU:        "привет <x>",
			EN:        "a greeting",
			Explain:   "так отмечают встречу",
			Context:   "когда встречаешь человека",
			Contrast:  "не прощание",
			Grammar:   "часто без артикля",
			Formality: "нейтрально",
			Synonyms:  []string{"hi"},
			Examples: []dict.Example{
				{EN: "Hello, world!", RU: "Привет, мир!"},
			},
		}},
		Source:       "llm",
		Tip:          "ассоциация",
		Collocations: []string{"hello there"},
		Mistake:      "не hello как существительное в письме без нужды",
	}, true)
	if strings.Contains(got, "<x>") {
		t.Fatalf("unescaped: %s", got)
	}
	if !strings.Contains(got, "A1") || !strings.Contains(got, "a greeting") || !strings.Contains(got, "Hello, world!") {
		t.Fatalf("got %s", got)
	}
	if !strings.Contains(got, "когда") || !strings.Contains(got, "так отмечают") {
		t.Fatalf("missing explanation: %s", got)
	}
	if !strings.Contains(got, "Суть") || !strings.Contains(got, "Антонимы") {
		t.Fatalf("missing overview: %s", got)
	}
	if !strings.Contains(got, "Как запомнить") || !strings.Contains(got, "Часто вместе") {
		t.Fatalf("missing study extras: %s", got)
	}
}

func TestFormatDictWithoutArticle(t *testing.T) {
	got := formatDict(dict.Result{Normalized: "foo bar", Translation: "фу бар"}, true)
	if !strings.Contains(got, "фу бар") {
		t.Fatalf("got %s", got)
	}
}
