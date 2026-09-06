package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appbot "engbot/internal/bot"
	"engbot/internal/dict"
	"engbot/internal/store"

	"github.com/joho/godotenv"
	tele "gopkg.in/telebot.v3"

	_ "time/tzdata"
)

const placeholderToken = "123456:ABC-your-token-from-BotFather"

func main() {
	loadEnv()
	logFile := setupLogging()
	defer logFile.Close()

	token := botToken()
	if !tokenOK(token) {
		log.Fatal("нет BOT_TOKEN в окружении или .env")
	}
	dbPath := resolveDBPath()
	tz := getenv("DEFAULT_TZ", "Europe/Moscow")
	if _, err := time.LoadLocation(tz); err != nil {
		log.Printf("DEFAULT_TZ %q некорректен, беру Europe/Moscow: %v", tz, err)
		tz = "Europe/Moscow"
	}

	st, err := store.Open(dbPath, tz)
	if err != nil {
		fallback := filepath.Join(os.TempDir(), "engbot-data", "engbot.json")
		log.Printf("база %s недоступна (%v), пробую %s", dbPath, err, fallback)
		st, err = store.Open(fallback, tz)
		if err != nil {
			log.Fatal(err)
		}
		dbPath = fallback
	}
	defer st.Close()

	b, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 15 * time.Second},
		Client: telegramHTTPClient(),
	})
	if err != nil {
		log.Fatal(err)
	}

	app := appbot.New(b, st, newDictClient(dbPath))
	go app.RunReminders()
	go runBackupLoop(dbPath, tz)
	if strings.TrimSpace(os.Getenv("GROQ_API_KEY")) != "" {
		log.Print("словарь: Groq включён")
	} else {
		log.Print("словарь: нет GROQ_API_KEY, значения будут короче")
	}
	serveHealth()
	log.Printf("бот запущен, база %s, пояс %s", dbPath, tz)
	b.Start()
}

func telegramHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   20 * time.Second,
			ResponseHeaderTimeout: 45 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

func serveHealth() {
	port := getenv("PORT", "")
	if port == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	addr := "0.0.0.0:" + port
	go func() {
		log.Printf("health http %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("health http: %v", err)
		}
	}()
}

func botToken() string {
	for _, k := range []string{"BOT_TOKEN", "TELEGRAM_BOT_TOKEN", "TELEGRAM_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); tokenOK(v) {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("BOT_TOKEN"))
}

func resolveDataDir() string {
	if d := getenv("DATA_DIR", ""); d != "" {
		return d
	}
	if st, err := os.Stat("/app/data"); err == nil && st.IsDir() {
		return "/app/data"
	}
	return "data"
}

func resolveDBPath() string {
	if p := getenv("DATABASE_PATH", ""); p != "" {
		return p
	}
	return filepath.Join(resolveDataDir(), "engbot.json")
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func newDictClient(dbPath string) *dict.Client {
	c := dict.New(nil)
	c.GroqKey = strings.TrimSpace(os.Getenv("GROQ_API_KEY"))
	c.GroqModel = getenv("GROQ_MODEL", "")
	dir := filepath.Dir(dbPath)
	if dir == "." || dir == "" {
		dir = resolveDataDir()
	}
	c.CachePath = filepath.Join(dir, "wordcache.json")
	return c
}

func tokenOK(token string) bool {
	token = strings.TrimSpace(token)
	return token != "" && token != placeholderToken
}

func setupLogging() io.Closer {
	log.SetFlags(log.Ldate | log.Ltime)
	var candidates []string
	if p := getenv("LOG_PATH", ""); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		filepath.Join(resolveDataDir(), "engBot.log"),
		"engBot.log",
		filepath.Join(os.TempDir(), "engBot.log"),
	)
	seen := map[string]struct{}{}
	for _, path := range candidates {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		w, err := newLogRotator(path, defaultLogMaxBytes, defaultLogKeep)
		if err != nil {
			log.Printf("лог-файл %s недоступен: %v", path, err)
			continue
		}
		log.SetOutput(io.MultiWriter(os.Stderr, w))
		log.Printf("логи пишутся в %s (ротация по дню и при размере > %d байт, хранить %d архивов)", path, defaultLogMaxBytes, defaultLogKeep)
		return w
	}
	log.SetOutput(os.Stderr)
	log.Print("лог-файл недоступен, пишу только в stderr")
	return nopCloser{}
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func loadEnv() {
	p := findEnvFile()
	if p == "" {
		log.Print("env-файл не найден, читаю переменные окружения")
		return
	}
	if err := godotenv.Load(p); err != nil {
		log.Printf("не удалось загрузить env-файл: %v", err)
		return
	}
	log.Printf("загружен env-файл %s", p)
}

func findEnvFile() string {
	seen := map[string]struct{}{}
	for _, root := range envSearchRoots() {
		dir := root
		for i := 0; i < 6; i++ {
			p := filepath.Join(dir, ".env")
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

func envSearchRoots() []string {
	var roots []string
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	return roots
}
