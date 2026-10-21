package media

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/httpx"
)

// MovieAsset serves one file of a movie's permanent artwork from data/assets.
// Pure disk — never a provider call and never the cache: this is the render
// path the local-first design promises works with everything down. file is
// the path below the movie's directory ("poster.jpg",
// "gallery/backdrops/abc.thumb.jpg"); a miss is 404, which the SPA never
// requests because URLs are only minted for files already on disk.
func (s *Service) MovieAsset(ctx context.Context, idStr, file string) ([]byte, string, error) {
	return s.itemAsset(ctx, assets.MovieDir, idStr, file)
}

// ShowAsset serves one file of a SHOW's permanent artwork, which includes its
// seasons' posters and its episodes' stills: they live under the show's
// directory, so one route covers the whole subtree and "seasons/2/poster.jpg"
// or "episodes/2-7/still.thumb.jpg" needs nothing special.
//
// Deliberately no season or episode route of its own. One would have to carry
// seasons.id — what the SPA holds — and the handler would then need a database
// read on a pure-disk render path just to translate it into the show id and
// season number the path is built from.
func (s *Service) ShowAsset(ctx context.Context, idStr, file string) ([]byte, string, error) {
	return s.itemAsset(ctx, assets.ShowDir, idStr, file)
}

// itemAsset is the shared body: validate the id, reject anything that is not a
// plain relative image path, read from the tree.
func (s *Service) itemAsset(ctx context.Context, dir func(int64) string, idStr, file string) ([]byte, string, error) {
	_ = ctx
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id < 1 {
		return nil, "", fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
	}
	// A hyphen is neither a separator nor a dot run, so an episode directory
	// name like "2-7" passes this unchanged.
	for _, part := range strings.Split(file, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `\`) {
			return nil, "", fmt.Errorf("%w: invalid asset path", httpx.ErrValidation)
		}
	}
	if !strings.HasSuffix(file, ".jpg") && !strings.HasSuffix(file, ".jpeg") && !strings.HasSuffix(file, ".png") {
		return nil, "", fmt.Errorf("%w: not an image path", httpx.ErrValidation)
	}

	data, ok, err := s.Tree.Read(dir(id) + "/" + file)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("%w: no such asset", httpx.ErrNotFound)
	}
	return data, posterContentType(file), nil
}
