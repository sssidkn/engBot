package dict

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeCEFR(t *testing.T) {
	if got := NormalizeCEFR(" b1 "); got != "B1" {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeCEFR("A2-B1"); got != "A2–B1" {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeCEFR("nope"); got != "" {
		t.Fatalf("got %s", got)
	}
}

func TestEstimateCEFR(t *testing.T) {
	lv, _ := EstimateCEFR("hello")
	if lv != "A1" {
		t.Fatalf("hello=%s", lv)
	}
	lv, _ = EstimateCEFR("ubiquitous")
	if lv != "C1" {
		t.Fatalf("ubiquitous=%s", lv)
	}
}

func TestParseLLMArticle(t *testing.T) {
	raw := "```json\n{\"headword\":\"run\",\"cefr\":\"A1\",\"cefr_why\":\"частое\",\"overview\":\"движение ногами\",\"senses\":[{\"pos\":\"verb\",\"ru\":\"бежать\",\"en\":\"move fast\",\"explain\":\"быстро перемещаться\",\"context\":\"спорт\",\"examples\":[{\"en\":\"I run.\",\"ru\":\"Я бегу.\"}]}]}\n```"
	art, err := parseLLMArticle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if art.Headword != "run" || art.Overview != "движение ногами" || len(art.Senses) != 1 || art.Senses[0].RU != "бежать" {
		t.Fatalf("%+v", art)
	}
}

const richRunJSON = `{"headword":"run","phonetic":"/rʌn/","cefr":"A1","cefr_why":"базовое движение в учебниках","overview":"Глагол про быстрое перемещение на ногах и ещё несколько переносных смыслов.","senses":[{"pos":"verb","ru":"бежать","en":"to move quickly","explain":"Человек быстро перемещается на ногах, обычно без транспорта и ради цели.","context":"движение","examples":[{"en":"I run daily.","ru":"Я бегаю каждый день."},{"en":"Run to the shop.","ru":"Беги в магазин."}]}]}`

func groqWrap(article string) []byte {
	return []byte(`{"choices":[{"message":{"content":` + jsonString(article) + `}}]}`)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestExplainViaGroq(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testdev" {
			http.Error(w, "auth", 401)
			return
		}
		_, _ = w.Write(groqWrap(richRunJSON))
	}))
	defer srv.Close()
	c := New(srv.Client())
	c.GroqKey = "testdev"
	c.GroqURL = srv.URL
	res, err := c.explain("run", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.CEFR != "A1" || len(res.Senses) != 1 || res.Senses[0].RU != "бежать" {
		t.Fatalf("%+v", res)
	}
	if len(res.Senses[0].Examples) != 2 {
		t.Fatalf("examples=%+v", res.Senses[0].Examples)
	}
}

func TestLookupPrefersLLM(t *testing.T) {
	var groqHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/en/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"word":"run","phonetic":"/rʌn/","meanings":[{"partOfSpeech":"verb","definitions":[{"definition":"to move fast"}]}]}]`)
	})
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(memoryResp{ResponseStatus: 200, ResponseData: struct {
			TranslatedText string `json:"translatedText"`
		}{TranslatedText: "бежать"}})
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		groqHits++
		_, _ = w.Write(groqWrap(richRunJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.Client())
	c.DictBase = srv.URL + "/en/"
	c.TransBase = srv.URL + "/get"
	c.GroqKey = "k"
	c.GroqURL = srv.URL + "/chat"
	res, err := c.Lookup("run")
	if err != nil {
		t.Fatal(err)
	}
	if groqHits == 0 || res.Source != "llm" || !strings.Contains(res.Senses[0].Context, "движение") {
		t.Fatalf("hits=%d res=%+v", groqHits, res)
	}
	res2, err := c.Lookup("Run")
	if err != nil {
		t.Fatal(err)
	}
	if groqHits != 1 {
		t.Fatalf("cache should skip groq, hits=%d", groqHits)
	}
	if !res2.Cached {
		t.Fatal("expected cache hit")
	}
}

func TestLookupLLMFailureDoesNotUseBareTranslation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(memoryResp{ResponseStatus: 200, ResponseData: struct {
			TranslatedText string `json:"translatedText"`
		}{TranslatedText: "присматривать за"}})
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", 503)
	})
	mux.HandleFunc("/en/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.Client())
	c.DictBase = srv.URL + "/en/"
	c.TransBase = srv.URL + "/get"
	c.GroqKey = "k"
	c.GroqURL = srv.URL + "/chat"
	res, err := c.Lookup("look after")
	if err == nil {
		t.Fatalf("expected error, got %+v", res)
	}
	if strings.Contains(res.Translation, "присматри") {
		t.Fatalf("must not leak MyMemory: %+v", res)
	}
}

func TestExplainRetriesWhenPhraseCollapsed(t *testing.T) {
	n := 0
	lookJSON := `{"headword":"look","overview":"Это просто смотреть глазами на предмет перед собой сейчас.","senses":[{"pos":"verb","ru":"смотреть","explain":"Направить взгляд на что-то, без ответственности за человека.","examples":[{"en":"Look!","ru":"Смотри!"}]}]}`
	afterJSON := `{"headword":"look after","overview":"Фразовый глагол про заботу и ответственность, а не про взгляд глазами.","compare":"look for — искать; take care of — близкий синоним","senses":[{"pos":"phrasal verb","ru":"присматривать, заботиться, ухаживать","explain":"Взять на себя ответственность за безопасность и нужды человека, животного или дела. Это не look at.","context":"дети, больные, дом","contrast":"не значит смотреть","pattern":"look after sb/sth","examples":[{"en":"Can you look after my cat?","ru":"Можешь присмотреть за моей кошкой?"},{"en":"She looks after her grandmother.","ru":"Она ухаживает за бабушкой."},{"en":"Look after yourself.","ru":"Береги себя."}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			_, _ = w.Write(groqWrap(lookJSON))
			return
		}
		_, _ = w.Write(groqWrap(afterJSON))
	}))
	defer srv.Close()
	c := New(srv.Client())
	c.GroqKey = "k"
	c.GroqURL = srv.URL
	res, err := c.explain("look after", "")
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 || res.Word != "look after" || !strings.Contains(res.Senses[0].RU, "заботи") {
		t.Fatalf("n=%d res=%+v", n, res)
	}
}

func TestHeadwordMatchesQuery(t *testing.T) {
	if !headwordMatchesQuery("run", "run") {
		t.Fatal("run")
	}
	if headwordMatchesQuery("look", "look after") {
		t.Fatal("must reject look")
	}
	if !headwordMatchesQuery("look after", "look after") {
		t.Fatal("phrase")
	}
}

func TestCanonicalGroqModel(t *testing.T) {
	if got := canonicalGroqModel("llama-3.3-70b-versatile"); got != defaultGroqModel {
		t.Fatalf("got %s", got)
	}
	if got := canonicalGroqModel("openai/gpt-oss-20b"); got != "openai/gpt-oss-20b" {
		t.Fatalf("got %s", got)
	}
}

func TestGroqFallsBackWhenModelMissing(t *testing.T) {
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req groqReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		models = append(models, req.Model)
		if req.Model != "openai/gpt-oss-20b" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist or you do not have access to it."}}`))
			return
		}
		_, _ = w.Write(groqWrap(richRunJSON))
	}))
	defer srv.Close()
	c := New(srv.Client())
	c.GroqKey = "k"
	c.GroqURL = srv.URL
	c.GroqModel = "openai/gpt-oss-120b"
	_, err := c.groqChat("hi", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 2 || models[len(models)-1] != "openai/gpt-oss-20b" {
		t.Fatalf("models=%v", models)
	}
	_, err = c.groqChat("hi", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := models[len(models)-1]; got != "openai/gpt-oss-20b" || len(models) != 3 {
		t.Fatalf("should remember working model, models=%v", models)
	}
}
