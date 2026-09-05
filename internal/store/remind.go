package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxReminders = 8

var (
	ErrBadTime          = fmt.Errorf("некорректное время")
	ErrTooManyReminders = fmt.Errorf("слишком много напоминаний (максимум %d)", MaxReminders)
)

type DueReminder struct {
	User   User
	Clock  string
	ChatID int64
	Thread int
}

func ParseClock(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", ":")
	s = strings.ReplaceAll(s, ",", ":")
	s = strings.ReplaceAll(s, " ", "")
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return "", ErrBadTime
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", ErrBadTime
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

func (s *Store) Reminders(userID int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(userID)
	out := append([]string(nil), u.Reminders...)
	return out
}

func (s *Store) NotifyTarget(userID int64) (chatID int64, thread int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.userLocked(userID)
	return u.NotifyChat, u.NotifyThread
}

func (s *Store) SetNotifyTarget(userID, chatID int64, thread int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u := s.userLocked(userID)
	if u.NotifyChat == chatID && u.NotifyThread == thread {
		return nil
	}
	u.NotifyChat = chatID
	u.NotifyThread = thread
	s.data.Users[k] = u
	return s.saveLocked()
}

func (s *Store) AddReminder(userID int64, clock string, chatID int64, thread int) ([]string, error) {
	norm, err := ParseClock(clock)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u := s.userLocked(userID)
	for _, t := range u.Reminders {
		if t == norm {
			u.NotifyChat = chatID
			u.NotifyThread = thread
			s.data.Users[k] = u
			return append([]string(nil), u.Reminders...), s.saveLocked()
		}
	}
	if len(u.Reminders) >= MaxReminders {
		return append([]string(nil), u.Reminders...), ErrTooManyReminders
	}
	u.Reminders = append(u.Reminders, norm)
	sort.Strings(u.Reminders)
	u.NotifyChat = chatID
	u.NotifyThread = thread
	s.data.Users[k] = u
	return append([]string(nil), u.Reminders...), s.saveLocked()
}

func (s *Store) RemoveReminder(userID int64, clock string) ([]string, error) {
	norm, err := ParseClock(clock)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u := s.userLocked(userID)
	out := make([]string, 0, len(u.Reminders))
	for _, t := range u.Reminders {
		if t != norm {
			out = append(out, t)
		}
	}
	u.Reminders = out
	if u.LastRemind != nil {
		delete(u.LastRemind, norm)
	}
	s.data.Users[k] = u
	return append([]string(nil), u.Reminders...), s.saveLocked()
}

func (s *Store) ClearReminders(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u := s.userLocked(userID)
	u.Reminders = nil
	u.LastRemind = nil
	s.data.Users[k] = u
	return s.saveLocked()
}

func (s *Store) DueReminders(now time.Time) []DueReminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []DueReminder
	for _, u := range s.data.Users {
		if len(u.Reminders) == 0 || u.NotifyChat == 0 {
			continue
		}
		loc := s.Location(u)
		local := now.In(loc)
		hhmm := local.Format("15:04")
		day := local.Format("2006-01-02")
		studied := false
		for _, d := range s.data.Checkins[key(u.ID)] {
			if d == day {
				studied = true
				break
			}
		}
		if studied {
			continue
		}
		for _, clock := range u.Reminders {
			if clock != hhmm {
				continue
			}
			if u.LastRemind[clock] == day {
				continue
			}
			out = append(out, DueReminder{
				User:   u,
				Clock:  clock,
				ChatID: u.NotifyChat,
				Thread: u.NotifyThread,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].User.ID != out[j].User.ID {
			return out[i].User.ID < out[j].User.ID
		}
		return out[i].Clock < out[j].Clock
	})
	return out
}

func (s *Store) MarkReminded(userID int64, clock, day string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(userID)
	u := s.userLocked(userID)
	if u.LastRemind == nil {
		u.LastRemind = map[string]string{}
	}
	if u.LastRemind[clock] == day {
		return nil
	}
	u.LastRemind[clock] = day
	s.data.Users[k] = u
	return s.saveLocked()
}
