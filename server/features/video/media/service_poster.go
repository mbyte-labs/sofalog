package media

import (
	"context"
	"fmt"
	"strings"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// proxySizes is the whitelist of TMDB variants this proxy will fetch. It is a
// closed set because size goes straight into the CDN URL and into the cache
// path — an open one would let a caller fill the disk with variants nothing
// renders.
var proxySizes = map[string]bool{
	common.PosterSizeThumb:   true,
	common.PosterSizeFull:    true,
	common.BackdropSizeThumb: true,
	common.BackdropSizeFull:  true,
	common.ProfileSizeFull:   true,
}

// Poster serves one TMDB image variant from the local cache, fetching it from
// TMDB on first request. Despite the name it is the feature's whole TMDB image
// proxy: search-result and details-page posters, cast photos, the media
// section's backdrop and poster galleries, and the person page's portrait and
// filmography posters all draw through it. The provider traffic rides the
// search and details call sites (SPEC §5): a filename only becomes requestable
// because a search result, a stored snapshot (D-015) or a live person view
// (D-027) already named it. Artwork of tracked items lives in data/assets and
// is served by the per-item asset routes, with this proxy as the fallback
// while the download queue drains; a person owns no data/assets directory, so
// this cache is the only place its images ever live.
//
// All variants share the ("tmdb", "posters", size, file) cache path. Backdrops
// living under a directory called "posters" is a mild misnomer and deliberately
// not worth a migration: size is a path element, so a backdrop's w300 can never
// collide with a poster's w185.
func (s *Service) Poster(ctx context.Context, size, file string) (data []byte, contentType string, err error) {
	if !proxySizes[size] {
		return nil, "", fmt.Errorf("%w: unsupported image size %q", httpx.ErrValidation, size)
	}
	if !common.PosterFileRe.MatchString(file) {
		return nil, "", fmt.Errorf("%w: not a TMDB image filename", httpx.ErrValidation)
	}
	contentType = posterContentType(file)

	data, ok, err := s.Cache.Read("tmdb", "posters", size, file)
	if err != nil {
		return nil, "", err
	}
	if ok {
		return data, contentType, nil
	}

	data, err = s.TMDB.DownloadImage(ctx, size, "/"+file)
	if err != nil {
		return nil, "", err
	}
	if err := s.Cache.Write(data, "tmdb", "posters", size, file); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// posterContentType derives the MIME type from the filename, so cache hits
// need no sidecar metadata. PosterFileRe has already constrained the extension.
func posterContentType(file string) string {
	if strings.HasSuffix(file, ".png") {
		return "image/png"
	}
	return "image/jpeg"
}
