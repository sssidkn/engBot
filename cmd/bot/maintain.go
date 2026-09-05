package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogMaxBytes = 2 << 20 // 2 MiB
	defaultLogKeep     = 7
	defaultBackupKeep  = 14
)

type logRotator struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	file     *os.File
	day      string
}

func newLogRotator(path string, maxBytes int64, keep int) (*logRotator, error) {
	if maxBytes <= 0 {
		maxBytes = defaultLogMaxBytes
	}
	if keep <= 0 {
		keep = defaultLogKeep
	}
	w := &logRotator{path: path, maxBytes: maxBytes, keep: keep}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *logRotator) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *logRotator) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *logRotator) open() error {
	if dir := filepath.Dir(w.path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.file = f
	w.day = time.Now().Format("2006-01-02")
	return nil
}

func (w *logRotator) rotateIfNeeded() error {
	if w.file == nil {
		return w.open()
	}
	day := time.Now().Format("2006-01-02")
	st, err := w.file.Stat()
	if err != nil {
		return err
	}
	bySize := st.Size() >= w.maxBytes
	byDay := w.day != "" && w.day != day
	if !bySize && !byDay {
		return nil
	}
	oldDay := w.day
	if oldDay == "" {
		oldDay = day
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	archived := filepath.Join(filepath.Dir(w.path), archiveLogName(filepath.Base(w.path), oldDay))
	archived = uniquePath(archived)
	if err := os.Rename(w.path, archived); err != nil && !os.IsNotExist(err) {
		_ = w.open()
		return err
	}
	if err := pruneFiles(filepath.Dir(w.path), logArchivePrefix(filepath.Base(w.path)), w.keep); err != nil {
		return err
	}
	return w.open()
}

func archiveLogName(base, day string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".log"
	}
	return fmt.Sprintf("%s-%s%s", stem, day, ext)
}

func logArchivePrefix(base string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return stem + "-"
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 100; i++ {
		p := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(p); err != nil && os.IsNotExist(err) {
			return p
		}
	}
	return fmt.Sprintf("%s-%d%s", stem, time.Now().Unix(), ext)
}

func pruneFiles(dir, prefix string, keep int) error {
	if keep <= 0 || dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return nil
	}
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func backupDB(dbPath, locName string) (string, bool, error) {
	loc, err := time.LoadLocation(locName)
	if err != nil || loc == nil {
		loc = time.Local
	}
	day := time.Now().In(loc).Format("2006-01-02")
	src, err := os.Open(dbPath)
	if err != nil {
		return "", false, err
	}
	defer src.Close()

	dir := filepath.Join(filepath.Dir(dbPath), "backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	dstPath := filepath.Join(dir, "engbot-"+day+".json")
	if _, err := os.Stat(dstPath); err == nil {
		return dstPath, false, nil
	}
	tmp := dstPath + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", false, err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", false, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", false, closeErr
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		_ = os.Remove(dstPath)
		if err2 := os.Rename(tmp, dstPath); err2 != nil {
			_ = os.Remove(tmp)
			return "", false, err2
		}
	}
	if err := pruneFiles(dir, "engbot-", defaultBackupKeep); err != nil {
		log.Printf("очистка старых бэкапов: %v", err)
	}
	return dstPath, true, nil
}

func runBackupLoop(dbPath, locName string) {
	do := func() {
		path, created, err := backupDB(dbPath, locName)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			log.Printf("бэкап базы: %v", err)
			return
		}
		if created {
			log.Printf("бэкап базы %s", path)
		}
	}
	do()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		do()
	}
}
