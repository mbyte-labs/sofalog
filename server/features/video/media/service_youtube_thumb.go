package media

import (
	"context"
	"fmt"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// YouTubeThumb serves one video's still frame from the local cache, fetching it
// from YouTube's thumbnail CDN on first request. It is the sibling of Poster and
// exists for the same reason: the SPA must never hold a provider URL, and a
// details page must never make the browser talk to a third party (D-017).
//
// Only keys already named by a stored snapshot are ever requested, so the
// provider traffic rides the details-snapshot call site the same way posters do.
// Frames are cached as <key>.jpg — mqdefault is always JPEG, so there is one
// variant and no size element in the path.
func (s *Service) YouTubeThumb(ctx context.Context, key string) (data []byte, contentType string, err error) {
	if !common.YouTubeKeyRe.MatchString(key) {
		return nil, "", fmt.Errorf("%w: not a YouTube video id", httpx.ErrValidation)
	}
	contentType = "image/jpeg"
	file := key + ".jpg"

	data, ok, err := s.Cache.Read("youtube", "thumbs", file)
	if err != nil {
		return nil, "", err
	}
	if ok {
		return data, contentType, nil
	}

	data, err = s.YouTube.Thumbnail(ctx, key)
	if err != nil {
		return nil, "", err
	}
	if err := s.Cache.Write(data, "youtube", "thumbs", file); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}
