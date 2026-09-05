package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"
)

type Store struct {
	path      string
	defaultTZ string
	mu        sync.Mutex
	data      db
}

type db struct {
	Users    map[string]User     `json:"users"`
	Checkins map[string][]string `json:"checkins"`
	Chats    map[string][]int64  `json:"chats"`
	Topics   map[string]int      `json:"topics"`
}

type User struct {
	ID           int64             `json:"id"`
	Username     string            `json:"username"`
	FirstName    string            `json:"first_name"`
	Timezone     string            `json:"timezone"`
	Reminders    []string          `json:"reminders,omitempty"`
	NotifyChat   int64             `json:"notify_chat,omitempty"`
	NotifyThread int               `json:"notify_thread,omitempty"`
	LastRemind   map[string]string `json:"last_remind,omitempty"`
}

type DayStatus string

const (
	DayStudied DayStatus = "studied"
	DayMissed  DayStatus = "missed"
	DayEmpty   DayStatus = "empty"
	DayToday   DayStatus = "today"
	DayFuture  DayStatus = "future"
)

type Stats struct {
	TotalDays     int
	CurrentStreak int
	BestStreak    int
	FirstDay      string
	LastDay       string
	ThisMonth     int
}

type MemberRow struct {
	User          User
	CurrentStreak int
	TotalDays     int
	DoneToday     bool
}

func Open(path, defaultTZ string) (*Store, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	s := &Store{
		path:      path,
		defaultTZ: defaultTZ,
		data: db{
			Users:    map[string]User{},
			Checkins: map[string][]string{},
			Chats:    map[string][]int64{},
			Topics:   map[string]int{},
		},
	}
	raw, _, err := readDBBytes(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, s.saveLocked()
		}
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("parse db: %w", err)
		}
	}
	s.ensureMaps()
	return s, nil
}

func readDBBytes(path string) (raw []byte, fromTmp bool, err error) {
	raw, err = os.ReadFile(path)
	if err == nil && len(raw) > 0 {
		return raw, false, nil
	}
	readErr := err
	tmp := path + ".tmp"
	rec, recErr := os.ReadFile(tmp)
	if recErr == nil && len(rec) > 0 {
		return rec, true, nil
	}
	if readErr != nil {
		return nil, false, readErr
	}
	return raw, false, nil
}

func (s *Store) ensureMaps() {
	if s.data.Users == nil {
		s.data.Users = map[string]User{}
	}
	if s.data.Checkins == nil {
		s.data.Checkins = map[string][]string{}
	}
	if s.data.Chats == nil {
		s.data.Chats = map[string][]int64{}
	}
	if s.data.Topics == nil {
		s.data.Topics = map[string]int{}
	}
}

var (
	ErrBadDay    = errors.New("некорректная дата")
	ErrFutureDay = errors.New("день ещё не наступил")
)

func (s *Store) Close() error { return nil }

func key(id int64) string { return strconv.FormatInt(id, 10) }

func (s *Store) saveLocked() error {
	s.ensureMaps()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return replaceFile(tmp, s.path)
}

// replaceFile puts tmp at dest. On Windows os.Rename cannot overwrite dest,
// so dest is moved aside first and restored if the final rename fails.
func replaceFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	bak := dest + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(dest, bak); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if err2 := os.Rename(bak, dest); err2 != nil && !os.IsNotExist(err2) {
			return fmt.Errorf("replace %s: %w (restore: %v)", dest, err, err2)
		}
		return err
	}
	_ = os.Remove(bak)
	return nil
}

func (s *Store) UpsertUser(id int64, username, firstName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(id)
	u, ok := s.data.Users[k]
	if !ok {
		u = User{ID: id, Timezone: s.defaultTZ}
	}
	u.Username = username
	u.FirstName = firstName
	s.data.Users[k] = u
	return s.saveLocked()
}

func (s *Store) TouchChat(chatID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := key(chatID)
	for _, existing := range s.data.Chats[ck] {
		if existing == userID {
			return nil
		}
	}
	s.data.Chats[ck] = append(s.data.Chats[ck], userID)
	return s.saveLocked()
}

func (s *Store) SetChatTopic(chatID int64, threadID int) error {
	if threadID == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Topics == nil {
		s.data.Topics = map[string]int{}
	}
	k := key(chatID)
	if s.data.Topics[k] == threadID {
		return nil
	}
	s.data.Topics[k] = threadID
	return s.saveLocked()
}

func (s *Store) ChatTopic(chatID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Topics[key(chatID)]
}

func (s *Store) GetUser(id int64) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.data.Users[key(id)]; ok {
		return u, nil
	}
	return User{ID: id, Timezone: s.defaultTZ}, nil
}

func (s *Store) SetTimezone(userID int64, tz string) error {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return fmt.Errorf("неизвестный часовой пояс: пустой")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("неизвестный часовой пояс: %s", tz)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u, ok := s.data.Users[k]
	if !ok {
		u = User{ID: userID, Timezone: tz}
	}
	u.Timezone = tz
	s.data.Users[k] = u
	return s.saveLocked()
}

func (s *Store) Location(user User) *time.Location {
	tz := strings.TrimSpace(user.Timezone)
	if tz == "" {
		tz = strings.TrimSpace(s.defaultTZ)
	}
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil && loc != nil {
			return loc
		}
	}
	if def := strings.TrimSpace(s.defaultTZ); def != "" && def != tz {
		if loc, err := time.LoadLocation(def); err == nil && loc != nil {
			return loc
		}
	}
	return time.UTC
}

func (s *Store) Today(user User) string {
	return time.Now().In(s.Location(user)).Format("2006-01-02")
}

func (s *Store) MarkToday(userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(userID)
	day := time.Now().In(s.Location(u)).Format("2006-01-02")
	return s.setDayLocked(userID, day, true)
}

func (s *Store) UnmarkToday(userID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(userID)
	day := time.Now().In(s.Location(u)).Format("2006-01-02")
	return s.setDayLocked(userID, day, false)
}

// ToggleDay marks or unmarks any day that is today or earlier in the user's timezone.
// The bool is whether the day is marked after the call.
func (s *Store) ToggleDay(userID int64, day string) (bool, error) {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return false, ErrBadDay
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(userID)
	today := time.Now().In(s.Location(u)).Format("2006-01-02")
	if day > today {
		return false, ErrFutureDay
	}
	k := key(userID)
	has := false
	for _, d := range s.data.Checkins[k] {
		if d == day {
			has = true
			break
		}
	}
	if _, err := s.setDayLocked(userID, day, !has); err != nil {
		return false, err
	}
	return !has, nil
}

func (s *Store) setDayLocked(userID int64, day string, wantMarked bool) (bool, error) {
	k := key(userID)
	days := s.data.Checkins[k]
	has := false
	for _, d := range days {
		if d == day {
			has = true
			break
		}
	}
	if wantMarked {
		if has {
			return false, nil
		}
		days = append(append([]string(nil), days...), day)
		sort.Strings(days)
		s.data.Checkins[k] = days
		return true, s.saveLocked()
	}
	if !has {
		return false, nil
	}
	out := make([]string, 0, len(days))
	for _, d := range days {
		if d == day {
			continue
		}
		out = append(out, d)
	}
	s.data.Checkins[k] = out
	return true, s.saveLocked()
}

func (s *Store) HasDay(userID int64, day string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.data.Checkins[key(userID)] {
		if d == day {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) Days(userID int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.data.Checkins[key(userID)]
	out := make([]string, len(src))
	copy(out, src)
	return out, nil
}

func (s *Store) userLocked(id int64) User {
	if u, ok := s.data.Users[key(id)]; ok {
		return u
	}
	return User{ID: id, Timezone: s.defaultTZ}
}

func (s *Store) Stats(userID int64) (Stats, error) {
	days, err := s.Days(userID)
	if err != nil {
		return Stats{}, err
	}
	u, err := s.GetUser(userID)
	if err != nil {
		return Stats{}, err
	}
	today := s.Today(u)
	st := Stats{
		TotalDays:     len(days),
		CurrentStreak: CurrentStreak(days, today),
		BestStreak:    BestStreak(days),
	}
	if len(days) > 0 {
		st.FirstDay = days[0]
		st.LastDay = days[len(days)-1]
	}
	if len(today) >= 7 {
		prefix := today[:7]
		for _, d := range days {
			if len(d) >= 7 && d[:7] == prefix {
				st.ThisMonth++
			}
		}
	}
	return st, nil
}

func (s *Store) MonthGrid(userID int64, year int, month time.Month) (map[int]DayStatus, error) {
	u, err := s.GetUser(userID)
	if err != nil {
		return nil, err
	}
	days, err := s.Days(userID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(days))
	for _, d := range days {
		set[d] = struct{}{}
	}
	firstStudy := ""
	if len(days) > 0 {
		firstStudy = days[0]
	}
	loc := s.Location(u)
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	last := first.AddDate(0, 1, -1)
	out := make(map[int]DayStatus, last.Day())
	for d := 1; d <= last.Day(); d++ {
		key := time.Date(year, month, d, 0, 0, 0, 0, loc).Format("2006-01-02")
		if _, ok := set[key]; ok {
			out[d] = DayStudied
			continue
		}
		if key > today {
			out[d] = DayFuture
			continue
		}
		if key == today {
			out[d] = DayToday
			continue
		}
		if firstStudy == "" || key < firstStudy {
			out[d] = DayEmpty
			continue
		}
		out[d] = DayMissed
	}
	return out, nil
}

func (s *Store) ChatBoard(chatID int64) ([]MemberRow, error) {
	s.mu.Lock()
	ids := append([]int64(nil), s.data.Chats[key(chatID)]...)
	s.mu.Unlock()

	out := make([]MemberRow, 0, len(ids))
	for _, id := range ids {
		u, err := s.GetUser(id)
		if err != nil {
			return nil, err
		}
		days, err := s.Days(id)
		if err != nil {
			return nil, err
		}
		today := s.Today(u)
		done, err := s.HasDay(id, today)
		if err != nil {
			return nil, err
		}
		out = append(out, MemberRow{
			User:          u,
			CurrentStreak: CurrentStreak(days, today),
			TotalDays:     len(days),
			DoneToday:     done,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CurrentStreak != out[j].CurrentStreak {
			return out[i].CurrentStreak > out[j].CurrentStreak
		}
		return out[i].TotalDays > out[j].TotalDays
	})
	return out, nil
}

func CurrentStreak(days []string, today string) int {
	if len(days) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(days))
	for _, d := range days {
		set[d] = struct{}{}
	}
	start := today
	if _, ok := set[today]; !ok {
		y, err := time.Parse("2006-01-02", today)
		if err != nil {
			return 0
		}
		start = y.AddDate(0, 0, -1).Format("2006-01-02")
		if _, ok := set[start]; !ok {
			return 0
		}
	}
	n := 0
	cur, err := time.Parse("2006-01-02", start)
	if err != nil {
		return 0
	}
	for {
		k := cur.Format("2006-01-02")
		if _, ok := set[k]; !ok {
			break
		}
		n++
		cur = cur.AddDate(0, 0, -1)
	}
	return n
}

func BestStreak(days []string) int {
	if len(days) == 0 {
		return 0
	}
	best, cur := 1, 1
	prev, err := time.Parse("2006-01-02", days[0])
	if err != nil {
		return 0
	}
	for i := 1; i < len(days); i++ {
		d, err := time.Parse("2006-01-02", days[i])
		if err != nil {
			cur = 1
			continue
		}
		if d.Equal(prev.AddDate(0, 0, 1)) {
			cur++
		} else {
			cur = 1
		}
		if cur > best {
			best = cur
		}
		prev = d
	}
	return best
}
