package common

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/db"
)

// The tracked-movie asset lifecycle. A tracked movie's full media set — hero
// poster and backdrop, both galleries, cast photos, video thumbnails — is
// downloaded into data/assets/video/movies/<id>/ through the queue, so the
// details page renders identically with TMDB down (SPEC §5, D-015). A refresh
// that changes the artwork deletes the replaced files and enqueues the new
// ones: no orphans.

// MovieFiles returns the movie's on-disk asset set for URL selection, or nil
// when there is nothing to consult (no local row, or no tree wired). Failures
// degrade to proxy URLs rather than failing the page.
func (s *Core) MovieFiles(movieID int64) map[string]bool {
	if s.Tree == nil || movieID < 1 {
		return nil
	}
	files, err := s.Tree.ListMovieFiles(movieID)
	if err != nil {
		slog.Warn("assets: listing movie files", "movie_id", movieID, "err", err)
		return nil
	}
	return files
}

// computeDesiredMovieAssets derives the full file set a tracked movie owes
// from its snapshot — through the same RankedImages/RankedVideos projections
// the DTO renders, so the downloaded set and the page can never diverge.
// Keyed by dest path; the map IS the diff key set.
func computeDesiredMovieAssets(row MovieRow) map[string]assets.Job {
	desired := map[string]assets.Job{}
	add := func(kind, size, sourceSize, file, dest string) {
		// Only proxy-shaped provider filenames become jobs — anything else
		// could not have been rendered either.
		if !PosterFileRe.MatchString(file) {
			return
		}
		desired[dest] = assets.Job{
			Provider:  "tmdb",
			MediaType: "movie",
			ItemID:    row.ID,
			Kind:      kind,
			Size:      size,
			SourceURL: sourceSize + "/" + file,
			DestPath:  dest,
		}
	}

	if row.PosterPath != nil {
		base := strings.TrimPrefix(*row.PosterPath, "/")
		add("poster", "thumb", PosterSizeThumb, base, assets.MoviePoster(row.ID, true))
		add("poster", "full", PosterSizeFull, base, assets.MoviePoster(row.ID, false))
	}
	if row.BackdropPath != nil {
		base := strings.TrimPrefix(*row.BackdropPath, "/")
		add("backdrop", "thumb", BackdropSizeThumb, base, assets.MovieBackdrop(row.ID, true))
		add("backdrop", "full", BackdropSizeFull, base, assets.MovieBackdrop(row.ID, false))
	}

	var snap MediaSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)

	for _, img := range RankedImages(snap.Images.Backdrops) {
		base := strings.TrimPrefix(img.FilePath, "/")
		add("gallery_backdrop", "thumb", BackdropSizeThumb, base,
			assets.MovieGalleryImage(row.ID, "backdrops", base, true))
		add("gallery_backdrop", "full", BackdropSizeFull, base,
			assets.MovieGalleryImage(row.ID, "backdrops", base, false))
	}
	for _, img := range RankedImages(snap.Images.Posters) {
		base := strings.TrimPrefix(img.FilePath, "/")
		add("gallery_poster", "thumb", PosterSizeThumb, base,
			assets.MovieGalleryImage(row.ID, "posters", base, true))
		add("gallery_poster", "full", PosterSizeFull, base,
			assets.MovieGalleryImage(row.ID, "posters", base, false))
	}
	for _, member := range snap.Credits.Cast {
		if member.ProfilePath == "" {
			continue
		}
		base := strings.TrimPrefix(member.ProfilePath, "/")
		add("cast", "single", PosterSizeThumb, base, assets.MovieCastPhoto(row.ID, base))
	}
	for _, v := range RankedVideos(snap.Videos.Results) {
		if !YouTubeKeyRe.MatchString(v.Key) {
			continue
		}
		dest := assets.MovieVideoThumb(row.ID, v.Key)
		desired[dest] = assets.Job{
			Provider:  "youtube",
			MediaType: "movie",
			ItemID:    row.ID,
			Kind:      "video_thumb",
			Size:      "single",
			SourceURL: v.Key,
			DestPath:  dest,
		}
	}
	return desired
}

// SyncMovieAssets reconciles a movie's permanent artwork with its current
// snapshot. Called after tracking and after every successful snapshot refresh.
//
// Only referenced movies own permanent artwork; an unreferenced row keeps
// riding the cache proxy (and its stale assets await the D-008 sweep).
func (s *Core) SyncMovieAssets(ctx context.Context, row MovieRow) {
	if s.Tree == nil || s.Queue == nil {
		return
	}
	referenced, err := MovieReferenced(ctx, s.DB, row.ID)
	if err != nil || !referenced {
		if err != nil {
			slog.Warn("assets: reference check failed, skipping sync", "movie_id", row.ID, "err", err)
		}
		return
	}
	s.syncItemAssets(ctx, "movie", row.ID, assets.MovieDir(row.ID), computeDesiredMovieAssets(row))
}

// syncItemAssets is the reconciler itself, shared by movies and shows: cancel
// jobs and delete files the current snapshot no longer names, enqueue what is
// missing, kick the worker.
//
// desired must be COMPLETE for the directory — every file the item owes, keyed
// by dest path. That is load-bearing: anything on disk under dir and absent
// from desired is deleted, so a partial desired set against a whole-directory
// listing destroys the rest. It is also why a show reconciles as one unit
// rather than per season.
//
// Deliberately best-effort — a failure is logged, never surfaced: the page
// still renders through the cache proxy, and the next track action or refresh
// re-diffs to convergence, which is also what heals a crash mid-sync.
func (s *Core) syncItemAssets(ctx context.Context, mediaType string, itemID int64, dir string, desired map[string]assets.Job) {
	onDisk, err := s.Tree.ListItemFiles(dir)
	if err != nil {
		slog.Warn("assets: listing files, skipping sync", "media_type", mediaType, "item_id", itemID, "err", err)
		return
	}
	sources, err := assets.ListJobSources(ctx, s.DB, mediaType, itemID)
	if err != nil {
		slog.Warn("assets: listing jobs, skipping sync", "media_type", mediaType, "item_id", itemID, "err", err)
		return
	}

	toEnqueue := make([]assets.Job, 0, len(desired))
	for dest, job := range desired {
		// A file is owed when it is missing — or present but downloaded from a
		// source the snapshot replaced (hero slots keep their dest when the
		// artwork changes, so the bytes go stale in place).
		if !onDisk[dest] || sources[dest] != job.SourceURL {
			toEnqueue = append(toEnqueue, job)
		}
	}
	// The job diff, computed here rather than as a NOT IN over the keep set —
	// see DeleteJobsForItemDests for why. Usually empty.
	toDrop := make([]string, 0)
	for dest := range sources {
		if _, wanted := desired[dest]; !wanted {
			toDrop = append(toDrop, dest)
		}
	}

	now := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := assets.DeleteJobsForItemDests(ctx, tx, mediaType, itemID, toDrop); err != nil {
			return err
		}
		return assets.UpsertJobs(ctx, tx, toEnqueue, now)
	})
	if err != nil {
		slog.Warn("assets: reconciling jobs failed", "media_type", mediaType, "item_id", itemID, "err", err)
		return
	}

	// Disk deletes come after the job rows are settled: a crash here leaves
	// extra files the next sync removes, never a missing job.
	for file := range onDisk {
		if _, wanted := desired[file]; wanted {
			continue
		}
		if err := s.Tree.RemoveItemFile(dir, file); err != nil {
			slog.Warn("assets: removing replaced file", "media_type", mediaType,
				"item_id", itemID, "file", file, "err", err)
		}
	}

	if len(toEnqueue) > 0 {
		slog.Info("assets: enqueued downloads", "media_type", mediaType,
			"item_id", itemID, "jobs", len(toEnqueue))
		s.Queue.Kick()
	}
}
