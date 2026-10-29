package common

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"mbyte.ro/sofalog/assets"
)

// The tracked-show asset lifecycle, the show half of service_assets.go. A
// tracked show's full media set — hero poster and backdrop, both galleries,
// cast photos, video thumbnails, EVERY season poster and EVERY episode still —
// is downloaded into data/assets/video/shows/<id>/ through the queue, so the
// show, season and episode pages all render identically with TMDB down.
//
// That is a few hundred files for a long-running show, which SPEC §6 calls out
// as intended and is exactly why downloads are queued and throttled.

// ShowFiles returns the show's on-disk asset set for URL selection, or nil when
// there is nothing to consult. Failures degrade to proxy URLs rather than
// failing the page.
func (s *Core) ShowFiles(showID int64) map[string]bool {
	if s.Tree == nil || showID < 1 {
		return nil
	}
	files, err := s.Tree.ListShowFiles(showID)
	if err != nil {
		slog.Warn("assets: listing show files", "show_id", showID, "err", err)
		return nil
	}
	return files
}

// ComputeDesiredShowAssets derives the full file set a tracked show owes.
//
// The show's own artwork comes from its snapshot through the same
// RankedImages/RankedVideos projections the DTO renders, so the downloaded set
// and the page cannot diverge — identical to the movie path. Season posters and
// episode stills come from the seasons.poster_path and episodes.still_path
// COLUMNS, which is both what the season DTO renders and far cheaper than
// parsing every season's metadata_json.
//
// EVERY job is filed as media_type='show', item_id=<show_id>, season posters
// and episode stills included. That is not a shortcut: the whole subtree
// reconciles as ONE unit because the show is the unit of deletion (D-008), and
// syncItemAssets deletes anything on disk that is absent from this map. A
// season poster filed as ('season', seasons.id) would be invisible to the diff
// and orphaned forever. The 'season' and 'episode' values in asset_jobs'
// media_type CHECK stay deliberately unused.
func ComputeDesiredShowAssets(row ShowRow, seasonPosters map[int]string, stills map[[2]int]string) map[string]assets.Job {
	desired := map[string]assets.Job{}
	add := func(kind, size, sourceSize, file, dest string) {
		// Only proxy-shaped provider filenames become jobs — anything else
		// could not have been rendered either.
		if !PosterFileRe.MatchString(file) {
			return
		}
		desired[dest] = assets.Job{
			Provider:  "tmdb",
			MediaType: "show",
			ItemID:    row.ID,
			Kind:      kind,
			Size:      size,
			SourceURL: sourceSize + "/" + file,
			DestPath:  dest,
		}
	}

	if row.PosterPath != nil {
		base := strings.TrimPrefix(*row.PosterPath, "/")
		add("poster", "thumb", PosterSizeThumb, base, assets.ShowPoster(row.ID, true))
		add("poster", "full", PosterSizeFull, base, assets.ShowPoster(row.ID, false))
	}
	if row.BackdropPath != nil {
		base := strings.TrimPrefix(*row.BackdropPath, "/")
		add("backdrop", "thumb", BackdropSizeThumb, base, assets.ShowBackdrop(row.ID, true))
		add("backdrop", "full", BackdropSizeFull, base, assets.ShowBackdrop(row.ID, false))
	}

	var snap MediaSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)

	for _, img := range RankedImages(snap.Images.Backdrops) {
		base := strings.TrimPrefix(img.FilePath, "/")
		add("gallery_backdrop", "thumb", BackdropSizeThumb, base,
			assets.ShowGalleryImage(row.ID, "backdrops", base, true))
		add("gallery_backdrop", "full", BackdropSizeFull, base,
			assets.ShowGalleryImage(row.ID, "backdrops", base, false))
	}
	for _, img := range RankedImages(snap.Images.Posters) {
		base := strings.TrimPrefix(img.FilePath, "/")
		add("gallery_poster", "thumb", PosterSizeThumb, base,
			assets.ShowGalleryImage(row.ID, "posters", base, true))
		add("gallery_poster", "full", PosterSizeFull, base,
			assets.ShowGalleryImage(row.ID, "posters", base, false))
	}
	// ShowCastEntries, not a raw section: the DTO reads the cast through the
	// same function, so the photos on disk are exactly the ones the page names
	// — aggregate credits when the snapshot has them, the legacy section else.
	for _, member := range ShowCastEntries(snap) {
		if member.ProfilePath == "" {
			continue
		}
		base := strings.TrimPrefix(member.ProfilePath, "/")
		add("cast", "single", PosterSizeThumb, base, assets.ShowCastPhoto(row.ID, base))
	}
	for _, v := range RankedVideos(snap.Videos.Results) {
		if !YouTubeKeyRe.MatchString(v.Key) {
			continue
		}
		dest := assets.ShowVideoThumb(row.ID, v.Key)
		desired[dest] = assets.Job{
			Provider:  "youtube",
			MediaType: "show",
			ItemID:    row.ID,
			Kind:      "video_thumb",
			Size:      "single",
			SourceURL: v.Key,
			DestPath:  dest,
		}
	}

	// Season 0 (Specials) gets its poster like any other season: D-012 excludes
	// specials from completion, not from the library.
	for number, path := range seasonPosters {
		base := strings.TrimPrefix(path, "/")
		add("season_poster", "thumb", PosterSizeThumb, base, assets.ShowSeasonPoster(row.ID, number, true))
		add("season_poster", "full", PosterSizeFull, base, assets.ShowSeasonPoster(row.ID, number, false))
	}
	// StillSizeSingle/StillSizeFull rather than the backdrop constants they
	// currently equal, so the intent reads at the call site.
	for key, path := range stills {
		base := strings.TrimPrefix(path, "/")
		add("still", "thumb", StillSizeSingle, base, assets.ShowEpisodeStill(row.ID, key[0], key[1], true))
		add("still", "full", StillSizeFull, base, assets.ShowEpisodeStill(row.ID, key[0], key[1], false))
	}
	return desired
}

// SyncShowAssets reconciles a tracked show's permanent artwork with its current
// snapshot. Called after every tracking action and after a successful show
// snapshot refresh; best-effort, exactly like SyncMovieAssets.
//
// Only referenced shows own permanent artwork — and a show is referenced six
// ways (ShowReferenced), not four: a rating on one of its episodes is enough.
func (s *Core) SyncShowAssets(ctx context.Context, row ShowRow) {
	if s.Tree == nil || s.Queue == nil {
		return
	}
	referenced, err := ShowReferenced(ctx, s.DB, row.ID)
	if err != nil || !referenced {
		if err != nil {
			slog.Warn("assets: reference check failed, skipping sync", "show_id", row.ID, "err", err)
		}
		return
	}

	posters, err := seasonPosterPaths(ctx, s.DB, row.ID)
	if err != nil {
		slog.Warn("assets: listing season posters, skipping sync", "show_id", row.ID, "err", err)
		return
	}
	stills, err := episodeStillPaths(ctx, s.DB, row.ID)
	if err != nil {
		slog.Warn("assets: listing episode stills, skipping sync", "show_id", row.ID, "err", err)
		return
	}
	s.syncItemAssets(ctx, "show", row.ID, assets.ShowDir(row.ID),
		ComputeDesiredShowAssets(row, posters, stills))
}
