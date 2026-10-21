package assets

import (
	"fmt"
	"path"
	"strings"
)

// The asset path builders. NOTHING outside this file may concatenate an asset
// path (SPEC §5) — every dest_path in asset_jobs, every Tree access and every
// diff key comes from here, so the on-disk layout is defined exactly once.
//
// All paths are relative to the data directory and start with "assets/", which
// is how the DB stores them. Layout for one movie (deleting it is one
// RemoveAll of MovieDir; exporting is a directory copy):
//
//	assets/video/movies/<movie_id>/
//	├── poster.jpg (w500)          poster.thumb.jpg (w185)
//	├── backdrop.jpg (w1280)       backdrop.thumb.jpg (w300)
//	├── gallery/backdrops/<file>   full + .thumb variants (w1280 / w300)
//	├── gallery/posters/<file>     full + .thumb variants (w500 / w185)
//	├── cast/<file>                single size (w185 — the page renders no larger)
//	└── videos/<key>.jpg           single size (D-017)
//
// A show carries the same six slots plus its seasons and episodes, all beneath
// ONE directory so that deleting a show is still a single RemoveAll (D-008) and
// exporting it is still one directory copy:
//
//	assets/video/shows/<show_id>/
//	├── poster.jpg  backdrop.jpg  gallery/…  cast/…  videos/…   (as above)
//	├── seasons/<season_number>/poster.jpg     poster.thumb.jpg
//	└── episodes/<season>-<episode>/still.jpg  still.thumb.jpg
//
// Note which levels are NOT database ids: seasons and episodes are addressed on
// disk by their POSITIONAL numbers (SPEC §5). A season is seasons.id when it is
// the target of a rating and season_number here — do not mix the two. The
// parameter types are the guard: an id is int64 and a number is int, so passing
// seasons.id where a season number belongs does not compile.
//
// There are deliberately no SeasonDir or EpisodeDir builders. Nothing may
// delete a season or an episode independently — pending_deletions admits only
// movie|show|game, because seasons and episodes cascade from their show.

// MovieDir is the movie's whole asset directory.
func MovieDir(movieID int64) string {
	return fmt.Sprintf("assets/video/movies/%d", movieID)
}

// MoviePoster and MovieBackdrop are the hero artwork slots. The provider
// filename is not part of the path — a replaced poster overwrites the same
// file, and the snapshot's poster_path names which provider image it is.
func MoviePoster(movieID int64, thumb bool) string {
	return MovieDir(movieID) + "/" + variant("poster.jpg", thumb)
}

func MovieBackdrop(movieID int64, thumb bool) string {
	return MovieDir(movieID) + "/" + variant("backdrop.jpg", thumb)
}

// MovieGalleryImage is one gallery entry. gallery is "backdrops" or "posters";
// file is the provider's bare filename ("abc.jpg" — no leading slash), kept so
// a refresh can diff the set image by image.
func MovieGalleryImage(movieID int64, gallery, file string, thumb bool) string {
	return MovieDir(movieID) + "/gallery/" + gallery + "/" + variant(file, thumb)
}

// MovieCastPhoto is one cast member's photo, single size.
func MovieCastPhoto(movieID int64, file string) string {
	return MovieDir(movieID) + "/cast/" + file
}

// MovieVideoThumb is one YouTube video still, single size (D-017).
func MovieVideoThumb(movieID int64, key string) string {
	return MovieDir(movieID) + "/videos/" + key + ".jpg"
}

// ShowDir is the show's whole asset directory — its own artwork, every
// season's poster and every episode's still live beneath it.
func ShowDir(showID int64) string {
	return fmt.Sprintf("assets/video/shows/%d", showID)
}

// ShowPoster and ShowBackdrop are the hero artwork slots, exactly as a movie's.
func ShowPoster(showID int64, thumb bool) string {
	return ShowDir(showID) + "/" + variant("poster.jpg", thumb)
}

func ShowBackdrop(showID int64, thumb bool) string {
	return ShowDir(showID) + "/" + variant("backdrop.jpg", thumb)
}

// ShowGalleryImage is one gallery entry; gallery is "backdrops" or "posters".
func ShowGalleryImage(showID int64, gallery, file string, thumb bool) string {
	return ShowDir(showID) + "/gallery/" + gallery + "/" + variant(file, thumb)
}

// ShowCastPhoto is one cast member's photo, single size.
func ShowCastPhoto(showID int64, file string) string {
	return ShowDir(showID) + "/cast/" + file
}

// ShowVideoThumb is one YouTube video still, single size (D-017).
func ShowVideoThumb(showID int64, key string) string {
	return ShowDir(showID) + "/videos/" + key + ".jpg"
}

// ShowSeasonPoster is one season's poster, keyed by SEASON NUMBER, not
// seasons.id. Season 0 is Specials and gets a folder like any other: D-012
// excludes specials from completion, not from caching.
func ShowSeasonPoster(showID int64, seasonNumber int, thumb bool) string {
	return fmt.Sprintf("%s/seasons/%d/%s", ShowDir(showID), seasonNumber, variant("poster.jpg", thumb))
}

// ShowEpisodeStill is one episode's still, keyed by the two positional numbers
// under the SHOW's directory rather than under the season's — that is what
// keeps a show's deletion a single RemoveAll.
//
// "<season>-<episode>" is unambiguous WITHOUT zero-padding, because both
// numbers are non-negative and rendered plainly: 1-23 is S1E23 and 12-3 is
// S12E3, and no two (season, episode) pairs can produce the same name. Do not
// "tidy" this into %02d — it would orphan every folder already on disk.
func ShowEpisodeStill(showID int64, season, episode int, thumb bool) string {
	return fmt.Sprintf("%s/episodes/%d-%d/%s", ShowDir(showID), season, episode, variant("still.jpg", thumb))
}

// variant appends the .thumb marker before the extension: "abc.jpg" →
// "abc.thumb.jpg". The two size variants of one image sit next to each other
// with the same base name.
func variant(file string, thumb bool) string {
	if !thumb {
		return file
	}
	ext := path.Ext(file)
	return strings.TrimSuffix(file, ext) + ".thumb" + ext
}
