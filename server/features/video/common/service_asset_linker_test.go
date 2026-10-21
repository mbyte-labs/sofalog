package common_test

import (
	"testing"

	"mbyte.ro/sofalog/features/video/common"
)

// The pair methods are what every card and page DTO addresses its artwork by:
// both provider sizes, each resolved on its own to the stored file or the
// cache proxy, and nil together when the row has no such image.
func TestAssetLinkerPairs(t *testing.T) {
	t.Parallel()
	path := "/p.jpg"

	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	check := func(t *testing.T, name string, thumb, full *string, wantThumb, wantFull string) {
		t.Helper()
		if str(thumb) != wantThumb || str(full) != wantFull {
			t.Errorf("%s = (%s, %s), want (%s, %s)", name, str(thumb), str(full), wantThumb, wantFull)
		}
	}

	t.Run("a nil path is nil, nil for every slot", func(t *testing.T) {
		link := common.ShowLinker(7, map[string]bool{"assets/video/shows/7/poster.jpg": true})
		thumb, full := link.Poster(nil)
		check(t, "Poster", thumb, full, "<nil>", "<nil>")
		thumb, full = link.Backdrop(nil)
		check(t, "Backdrop", thumb, full, "<nil>", "<nil>")
		thumb, full = link.SeasonPoster(1, nil)
		check(t, "SeasonPoster", thumb, full, "<nil>", "<nil>")
	})

	t.Run("no stored artwork proxies both sizes", func(t *testing.T) {
		link := common.MovieLinker(12, nil)
		thumb, full := link.Poster(&path)
		check(t, "Poster", thumb, full, "api/v1/video/posters/w185/p.jpg", "api/v1/video/posters/w500/p.jpg")
		thumb, full = link.Backdrop(&path)
		check(t, "Backdrop", thumb, full, "api/v1/video/posters/w300/p.jpg", "api/v1/video/posters/w1280/p.jpg")
	})

	t.Run("stored artwork serves both from the item's asset route", func(t *testing.T) {
		link := common.MovieLinker(12, map[string]bool{
			"assets/video/movies/12/poster.jpg":         true,
			"assets/video/movies/12/poster.thumb.jpg":   true,
			"assets/video/movies/12/backdrop.jpg":       true,
			"assets/video/movies/12/backdrop.thumb.jpg": true,
		})
		thumb, full := link.Poster(&path)
		check(t, "Poster", thumb, full, "api/v1/video/movies/12/assets/poster.thumb.jpg", "api/v1/video/movies/12/assets/poster.jpg")
		thumb, full = link.Backdrop(&path)
		check(t, "Backdrop", thumb, full, "api/v1/video/movies/12/assets/backdrop.thumb.jpg", "api/v1/video/movies/12/assets/backdrop.jpg")
	})

	t.Run("each size is picked on its own", func(t *testing.T) {
		// A half-synced item: the thumb landed, the full is still queued.
		link := common.ShowLinker(7, map[string]bool{"assets/video/shows/7/poster.thumb.jpg": true})
		thumb, full := link.Poster(&path)
		check(t, "Poster", thumb, full, "api/v1/video/shows/7/assets/poster.thumb.jpg", "api/v1/video/posters/w500/p.jpg")
	})

	t.Run("a show linker uses the show tree, seasons included", func(t *testing.T) {
		link := common.ShowLinker(7, map[string]bool{
			"assets/video/shows/7/backdrop.jpg":               true,
			"assets/video/shows/7/seasons/2/poster.jpg":       true,
			"assets/video/shows/7/seasons/2/poster.thumb.jpg": true,
		})
		thumb, full := link.Backdrop(&path)
		check(t, "Backdrop", thumb, full, "api/v1/video/posters/w300/p.jpg", "api/v1/video/shows/7/assets/backdrop.jpg")
		thumb, full = link.SeasonPoster(2, &path)
		check(t, "SeasonPoster", thumb, full, "api/v1/video/shows/7/assets/seasons/2/poster.thumb.jpg", "api/v1/video/shows/7/assets/seasons/2/poster.jpg")
	})
}
