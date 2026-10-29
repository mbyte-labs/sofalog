package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheReadWrite(t *testing.T) {
	t.Parallel()

	t.Run("a miss is ok=false, not an error", func(t *testing.T) {
		t.Parallel()
		c := NewCache(t.TempDir())
		data, ok, err := c.Read("tmdb", "posters", "w185", "missing.jpg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok || data != nil {
			t.Errorf("Read = (%q, %v), want a miss", data, ok)
		}
	})

	t.Run("write then read roundtrips, creating directories", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		c := NewCache(dir)
		if err := c.Write([]byte("bytes"), "tmdb", "posters", "w185", "a.jpg"); err != nil {
			t.Fatalf("Write: %v", err)
		}

		data, ok, err := c.Read("tmdb", "posters", "w185", "a.jpg")
		if err != nil || !ok {
			t.Fatalf("Read = (ok=%v, err=%v), want a hit", ok, err)
		}
		if string(data) != "bytes" {
			t.Errorf("data = %q", data)
		}

		// The layout on disk is part of the contract: cache/<provider>/…,
		// separate from assets/.
		onDisk := filepath.Join(dir, "cache", "tmdb", "posters", "w185", "a.jpg")
		if _, err := os.Stat(onDisk); err != nil {
			t.Errorf("expected file at %s: %v", onDisk, err)
		}
	})

	t.Run("no temp file survives a write", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		c := NewCache(dir)
		if err := c.Write([]byte("x"), "tmdb", "a.jpg"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(dir, "cache", "tmdb"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "a.jpg" {
			t.Errorf("directory holds %v, want only a.jpg", entries)
		}
	})

	// Traversal out of the cache root must be impossible by construction —
	// this is the boundary that keeps cache cleanup away from data/assets.
	invalid := [][]string{
		{},
		{""},
		{"."},
		{".."},
		{"..", "assets", "x.jpg"},
		{"a/b.jpg"},
		{`a\b.jpg`},
		{"tmdb", "../../assets/x.jpg"},
	}
	for _, parts := range invalid {
		t.Run("rejects invalid path", func(t *testing.T) {
			t.Parallel()
			c := NewCache(t.TempDir())
			if _, _, err := c.Read(parts...); err == nil {
				t.Errorf("Read(%q) accepted an invalid path", parts)
			}
			if err := c.Write([]byte("x"), parts...); err == nil {
				t.Errorf("Write(%q) accepted an invalid path", parts)
			}
		})
	}
}
