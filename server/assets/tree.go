package assets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Tree is the data/assets tree: permanent artwork of tracked items, keyed by
// internal db id. Structurally separate from Cache — the two share no delete
// helper, so no cache maintenance can ever be pointed at library artwork
// (SPEC §5).
type Tree struct {
	// root is the data directory; every stored path is relative to it and
	// starts with "assets/", matching what the DB stores.
	root string
}

// NewTree roots the assets tree at dataDir. Directories are created lazily on
// first write, so constructing one is side-effect free.
func NewTree(dataDir string) *Tree {
	return &Tree{root: dataDir}
}

// path validates rel — a slash-separated path from the builders in paths.go —
// and joins it under the root. Each element must be a single plain name;
// rejecting separators and dot-elements outright is what makes every resulting
// path provably stay inside assets/, with no prefix check to get subtly wrong.
func (t *Tree) path(rel string) (string, error) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "assets" {
		return "", fmt.Errorf("assets: path %q is not under assets/", rel)
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `\`) {
			return "", fmt.Errorf("assets: invalid path element %q", p)
		}
	}
	return filepath.Join(append([]string{t.root}, parts...)...), nil
}

// Read returns a stored file's contents. A miss is (nil, false, nil) — the
// normal state while the queue is still draining.
func (t *Tree) Read(rel string) ([]byte, bool, error) {
	p, err := t.path(rel)
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

// Write stores data at rel, creating directories as needed. Temp file plus
// rename, so a crash mid-write never leaves a truncated image that a later
// Read would serve as valid.
func (t *Tree) Write(data []byte, rel string) error {
	p, err := t.path(rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(p)+".tmp*")
	if err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("assets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("assets: %w", err)
	}
	return nil
}

// itemDir validates that dir names an ITEM directory — "assets/…/<numeric id>",
// three or four plain elements — which is the only shape ListItemFiles and
// RemoveItemFile may be pointed at.
//
// This exists because taking the directory as a parameter would otherwise
// weaken the guarantee the builders give: RemoveItemFile's prefix check is
// vacuous for dir="assets", which would turn it into "delete any asset file".
// Requiring plain elements ending in a numeric id is strictly stronger than the
// hardcoded MovieDir it replaces, and it is why a caller cannot walk or delete
// across a whole media type — or into part of one item, such as one season's
// folder inside a show.
//
// Three OR four, because the depth is per component, not universal (SPEC §5's
// layout): video holds two kinds, so its items live at
// "assets/video/movies/<id>", while games hold one and live at
// "assets/games/<id>". Hardcoding four silently rejected every game
// directory — and only at the moment one first owned a file, which is long
// after the path builder looked correct.
func itemDir(dir string) error {
	parts := strings.Split(dir, "/")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "assets" {
		return fmt.Errorf("assets: %q is not an item directory", dir)
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `\`) {
			return fmt.Errorf("assets: invalid path element %q", p)
		}
	}
	// The id is the LAST element whichever depth this component uses.
	for _, r := range parts[len(parts)-1] {
		if r < '0' || r > '9' {
			return fmt.Errorf("assets: %q does not end in a numeric item id", dir)
		}
	}
	return nil
}

// ListItemFiles returns the set of files currently stored for one item, as the
// same relative paths the builders produce — the disk half of the sync diff. An
// item with no directory yet is an empty set, not an error.
//
// For a show this walks the whole subtree, seasons and episodes included, which
// is deliberate: a show reconciles as ONE unit because the show is the unit of
// deletion (D-008), and the diff is only correct when the desired set and this
// listing cover identical ground.
func (t *Tree) ListItemFiles(dir string) (map[string]bool, error) {
	if err := itemDir(dir); err != nil {
		return nil, err
	}
	root := filepath.Join(append([]string{t.root}, strings.Split(dir, "/")...)...)

	files := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.Contains(d.Name(), ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(t.root, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = true
		return nil
	})
	if errors.Is(walkErr, os.ErrNotExist) {
		return files, nil
	}
	if walkErr != nil {
		return nil, fmt.Errorf("assets: %w", walkErr)
	}
	return files, nil
}

// RemoveItemFile deletes one of an item's files — the sync's cleanup of artwork
// a refresh replaced. rel must lie inside that item's directory; anything else
// is rejected before touching the filesystem. Removing an already-absent file
// is a no-op.
func (t *Tree) RemoveItemFile(dir, rel string) error {
	if err := itemDir(dir); err != nil {
		return err
	}
	if !strings.HasPrefix(rel, dir+"/") {
		return fmt.Errorf("assets: %q is outside %q", rel, dir)
	}
	p, err := t.path(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("assets: %w", err)
	}
	return nil
}

// The per-media-type spellings. They exist so a call site names what it is
// listing rather than assembling a directory itself — the paths.go rule.

func (t *Tree) ListMovieFiles(movieID int64) (map[string]bool, error) {
	return t.ListItemFiles(MovieDir(movieID))
}

func (t *Tree) RemoveMovieFile(movieID int64, rel string) error {
	return t.RemoveItemFile(MovieDir(movieID), rel)
}

func (t *Tree) ListShowFiles(showID int64) (map[string]bool, error) {
	return t.ListItemFiles(ShowDir(showID))
}

func (t *Tree) RemoveShowFile(showID int64, rel string) error {
	return t.RemoveItemFile(ShowDir(showID), rel)
}
