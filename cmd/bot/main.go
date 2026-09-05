package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	appbot "engbot/internal/bot"
	"engbot/internal/store"

	"github.com/joho/godotenv"
	tele "gopkg.in/telebot.v3"

	_ "time/tzdata"
)

const placeholderToken = "123456:ABC-your-token-from-BotFather"

func main() {
	loadEnv()
	token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if !tokenOK(token) {
		log.Fatal("нет токена. Скопируй .env.example в .env и вставь BOT_TOKEN от @BotFather")
	}
	dbPath := getenv("DATABASE_PATH", "data/engbot.json")
	tz := getenv("DEFAULT_TZ", "Europe/Moscow")
	if _, err := time.LoadLocation(tz); err != nil {
		log.Fatalf("DEFAULT_TZ: %v", err)
	}

	st, err := store.Open(dbPath, tz)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	b, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	app := appbot.New(b, st)
	go app.RunReminders()
	log.Printf("бот запущен, база %s, пояс %s", dbPath, tz)
	b.Start()
}

func tokenOK(token string) bool {
	token = strings.TrimSpace(token)
	return token != "" && token != placeholderToken
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
