package dict

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeAndLooksRussian(t *testing.T) {
	if got := NormalizeQuery("  Hello   world  "); got != "Hello world" {
		t.Fatalf("got %q", got)
	}
	if !LooksRussian("кофе") || LooksRussian("hello") {
		t.Fatal("language detect")
	}
}

func TestParseDictEntry(t *testing.T) {
	rawJSON := []byte(`{"word":"hello","phonetics":[{"text":"/həˈloʊ/"}],"meanings":[{"partOfSpeech":"noun","synonyms":["hi"],"definitions":[{"definition":"a greeting","example":"Hello, world!","synonyms":["howdy"]}]}]}`)
	var raw dictAPIEntry
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		t.Fatal(err)
	}
	e := parseDictEntry(raw)
	if e.Word != "hello" || e.Phonetic != "/həˈloʊ/" {
		t.Fatalf("%+v", e)
	}
	if len(e.Meanings) != 1 || e.Meanings[0].Definitions[0] != "a greeting" {
		t.Fatalf("meanings=%+v", e.Meanings)
	}
	if len(e.Meanings[0].Synonyms) < 2 {
		t.Fatalf("synonyms=%v", e.Meanings[0].Synonyms)
	}
}

func TestLookupEnglish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/en/") {
			_, _ = io.WriteString(w, `[{"word":"cat","phonetic":"/kæt/","meanings":[{"partOfSpeech":"noun","definitions":[{"definition":"a small domesticated carnivorous mammal","example":"the cat sat on the mat"}]}]}]`)
			return
		}
		if r.URL.Path == "/get" || strings.Contains(r.URL.RawQuery, "q=") {
			q := r.URL.Query().Get("q")
			tr := "кот"
			if q != "cat" {
				tr = "перевод:" + q
			}
			_ = json.NewEncoder(w).Encode(memoryResp{
				ResponseStatus: 200,
				ResponseData:   struct {
					TranslatedText string `json:"translatedText"`
				}{TranslatedText: tr},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(srv.Client())
	c.DictBase = srv.URL + "/en/"
	c.TransBase = srv.URL + "/get"
	res, err := c.Lookup("  cat ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Translation != "кот" || res.Word != "cat" || res.Phonetic != "/kæt/" {
		t.Fatalf("%+v", res)
	}
	if len(res.Meanings) != 1 {
		t.Fatalf("meanings=%+v", res.Meanings)
	}
}

func TestLookupRussianThenEnglish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/en/") {
			word := strings.TrimPrefix(r.URL.Path, "/en/")
			word, _ = url.PathUnescape(word)
			if word != "cat" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `[{"word":"cat","phonetic":"/kæt/","meanings":[{"partOfSpeech":"noun","definitions":[{"definition":"a feline"}]}]}]`)
			return
		}
		_ = json.NewEncoder(w).Encode(memoryResp{
			ResponseStatus: 200,
			ResponseData: struct {
				TranslatedText string `json:"translatedText"`
			}{TranslatedText: "cat"},
		})
	}))
	defer srv.Close()
	c := New(srv.Client())
	c.DictBase = srv.URL + "/en/"
	c.TransBase = srv.URL + "/get"
	res, err := c.Lookup("кот")
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromRussian || res.Translation != "cat" || res.Word != "cat" {
		t.Fatalf("%+v", res)
	}
}
