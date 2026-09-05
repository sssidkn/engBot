package dict

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxCacheEntries = 400

type cacheFile struct {
	Items map[string]cacheEntry `json:"items"`
}

type cacheEntry struct {
	At     time.Time `json:"at"`
	Result Result    `json:"result"`
}

func cacheKey(q string) string {
	return "v3:" + strings.ToLower(NormalizeQuery(q))
}

func (c *Client) cacheGet(q string) (Result, bool) {
	c.cacheOnce.Do(c.loadCache)
	key := cacheKey(q)
	if key == "" {
		return Result{}, false
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	ent, ok := c.cache[key]
	if !ok {
		return Result{}, false
	}
	res := ent.Result
	res.Cached = true
	return res, true
}

func (c *Client) cachePut(q string, res Result) {
	c.cacheOnce.Do(c.loadCache)
	key := cacheKey(q)
	if key == "" {
		return
	}
	res.Cached = false
	c.cacheMu.Lock()
	if c.cache == nil {
		c.cache = map[string]cacheEntry{}
	}
	c.cache[key] = cacheEntry{At: time.Now().UTC(), Result: res}
	c.pruneCacheLocked()
	c.cacheMu.Unlock()
	c.saveCache()
}

func (c *Client) pruneCacheLocked() {
	if len(c.cache) <= maxCacheEntries {
		return
	}
	type kv struct {
		k string
		t time.Time
	}
	list := make([]kv, 0, len(c.cache))
	for k, v := range c.cache {
		list = append(list, kv{k: k, t: v.At})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].t.Before(list[j].t) })
	drop := len(c.cache) - maxCacheEntries
	for i := 0; i < drop; i++ {
		delete(c.cache, list[i].k)
	}
}

func (c *Client) loadCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cache == nil {
		c.cache = map[string]cacheEntry{}
	}
	if strings.TrimSpace(c.CachePath) == "" {
		return
	}
	raw, err := os.ReadFile(c.CachePath)
	if err != nil || len(raw) == 0 {
		return
	}
	var f cacheFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Items == nil {
		return
	}
	c.cache = f.Items
}

func (c *Client) saveCache() {
	path := strings.TrimSpace(c.CachePath)
	if path == "" {
		return
	}
	c.cacheMu.Lock()
	payload := cacheFile{Items: c.cache}
	raw, err := json.MarshalIndent(payload, "", "  ")
	c.cacheMu.Unlock()
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
