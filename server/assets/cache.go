// Package assets manages sofalog's on-disk artwork. For now it holds only the
// temporary data/cache tree; the download queue, the asset path builders,
// cache pruning and the deletion sweep land in later milestones.
package assets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is the data/cache tree: temporary files keyed by provider id, kept
// separate from data/assets so that no cleanup of cached files can ever be
// pointed at library artwork.
//
// TODO(SPEC §5): TTL and max-total-size pruning land with the download queue.
type Cache struct {
	root string
}

// NewCache roots a cache at dataDir/cache. Directories are created lazily on
// first write, so constructing one is side-effect free.
func NewCache(dataDir string) *Cache {
	return &Cache{root: filepath.Join(dataDir, "cache")}
}

// path joins the parts under the cache root. Each part must be a single plain
// path element — rejecting separators and dot-elements outright is what makes
// every resulting path provably stay inside the root, with no prefix check to
// get subtly wrong.
func (c *Cache) path(parts []string) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("cache: a path needs at least one element")
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `/\`) {
			return "", fmt.Errorf("cache: invalid path element %q", p)
		}
	}
	return filepath.Join(append([]string{c.root}, parts...)...), nil
}

// Read returns a cached file's contents. A miss is (nil, false, nil) — not an
// error, because missing is the normal state before the first fetch.
func (c *Cache) Read(parts ...string) ([]byte, bool, error) {
	p, err := c.path(parts)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Write stores data at the given path, creating directories as needed. It
// writes a temp file and renames it into place, so a crash mid-write never
// leaves a truncated file that a later Read would serve as a valid image. Two
// concurrent writers of the same key both succeed; the bytes are identical
// (same provider file), so last-rename-wins is harmless.
func (c *Cache) Write(data []byte, parts ...string) error {
	p, err := c.path(parts)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(p)+".tmp*")
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	return nil
}
