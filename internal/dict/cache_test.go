package dict

import (
	"path/filepath"
	"testing"
)

func TestWordCacheRoundTrip(t *testing.T) {
	c := New(nil)
	c.CachePath = filepath.Join(t.TempDir(), "wordcache.json")
	res := Result{Word: "run", Source: "llm", Senses: []Sense{{RU: "бежать"}}}
	c.cachePut("Run", res)
	got, ok := c.cacheGet("run")
	if !ok || got.Word != "run" || !got.Cached {
		t.Fatalf("mem cache ok=%v %+v", ok, got)
	}
	c2 := New(nil)
	c2.CachePath = c.CachePath
	got, ok = c2.cacheGet("RUN")
	if !ok || got.Word != "run" {
		t.Fatalf("file cache ok=%v %+v", ok, got)
	}
}
