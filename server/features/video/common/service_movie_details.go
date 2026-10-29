package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/db"
)

// MediaGalleryMax bounds each tab of the details page's media section. TMDB
// carries well over a hundred posters and dozens of videos for a big release,
// and every tile the SPA renders is one fetch through an image proxy — the cap
// is what keeps a first view from queueing hundreds of downloads for artwork
// nobody scrolls to.
const MediaGalleryMax = 30

// CastMember is one credited actor on the details page. Nullable fields are
// pointers without omitempty — the SPA distinguishes null from absent.
type CastMember struct {
	// TMDBID is TMDB's person id — what the person page is addressed by
	// (D-027). Null when the stored body carries none, in which case the SPA
	// renders the card without a link; a real TMDB response always has it.
	TMDBID    *int64  `json:"tmdb_id"`
	Name      string  `json:"name"`
	Character *string `json:"character"`
	// Relative URL of the locally proxied photo (the w185 profile variant),
	// or null when TMDB has none. Never a provider URL.
	Photo *string `json:"photo"`
	// Episodes is how many episodes the actor appears in — a SHOW figure, from
	// TMDB's aggregate credits. Null for a movie, and null for a show snapshot
	// that still holds the old latest-season credits (see ShowCastEntries).
	Episodes *int `json:"episodes"`
}

// MovieVideo is one video TMDB lists for the movie — the media section's Videos
// tab. YouTube only; TMDB stores no frames of its own, so thumb is a relative
// URL of the locally proxied still (never a provider URL, D-017) and is null
// when the key is unusable. url is an outbound link the user leaves the app
// through, legitimately external like homepage and trailer_url.
type MovieVideo struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"` // Trailer | Teaser | Clip | Featurette | …
	Official    bool    `json:"official"`
	PublishedAt *string `json:"published_at"` // RFC3339
	URL         string  `json:"url"`
	Thumb       *string `json:"thumb"`
}

// MovieImage is one gallery image at both locally proxied variants — never a
// provider URL. width and height are the provider's dimensions for the full
// variant, so the SPA can lay a tile out at the right shape without waiting for
// the image to load; backdrops and posters have different aspect ratios and the
// same struct carries both.
type MovieImage struct {
	Thumb  string `json:"thumb"`
	Full   string `json:"full"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// MovieDetails is the details page's DTO, built entirely from the stored
// catalog snapshot — never straight from a provider response. Nullable fields
// are pointers without omitempty; slices are never null.
type MovieDetails struct {
	// Local catalog id, or null when the movie has no catalog row — an
	// untracked view served live from TMDB (D-015). Tracking it (mark watched,
	// watchlist) is what creates the row and makes the id non-null.
	ID            *int64   `json:"id"`
	TMDBID        int64    `json:"tmdb_id"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	ReleaseDate   *string  `json:"release_date"` // YYYY-MM-DD
	Year          *string  `json:"year"`         // "1999", derived from ReleaseDate
	RuntimeMin    *int     `json:"runtime_min"`
	Overview      *string  `json:"overview"`
	Tagline       *string  `json:"tagline"`
	Genres        []string `json:"genres"`
	// TMDB community score (0–10); null when TMDB reports 0 ("unrated").
	Rating   *float64     `json:"rating"`
	Votes    *int64       `json:"votes"`
	Director *string      `json:"director"`
	Cast     []CastMember `json:"cast"`
	// The media section, mirroring TMDB's own: videos, then the two image
	// galleries. Slices are never null — an empty one means TMDB lists none.
	Videos    []MovieVideo `json:"videos"`
	Backdrops []MovieImage `json:"backdrops"`
	Posters   []MovieImage `json:"posters"`
	// Relative URLs of the locally proxied poster (w185 thumb / w500 full),
	// or null when TMDB has none. Never a provider URL.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// Outbound references from the snapshot, null when TMDB has none. These
	// are links the user leaves the app through — unlike artwork they are
	// legitimately external. trailer_url is the best YouTube video: a trailer
	// beats a teaser, official beats unofficial, then newest wins.
	Homepage   *string `json:"homepage"`
	IMDBID     *string `json:"imdb_id"`
	TrailerURL *string `json:"trailer_url"`
	// Facts from the snapshot. budget and revenue are USD; TMDB reports 0 for
	// unknown, which maps to null. popularity is TMDB's relative trending
	// metric — fetched_at is when it was last refreshed.
	OriginalLanguage *string  `json:"original_language"` // ISO 639-1
	Budget           *int64   `json:"budget"`
	Revenue          *int64   `json:"revenue"`
	Popularity       *float64 `json:"popularity"`
	FetchedAt        string   `json:"fetched_at"` // RFC3339 UTC
	// Tracked reports whether any user references this movie (state, watchlist,
	// favorite, watch, rating or copy — the D-008 reference set). Watched,
	// Watchlisted, Favorited and UserRating are the requesting user's own
	// state, so the page controls initialise truthfully. All are false/null on
	// a live untracked view. Favorited is the third independent axis (D-030).
	Tracked     bool `json:"tracked"`
	Watched     bool `json:"watched"`
	Watchlisted bool `json:"watchlisted"`
	Favorited   bool `json:"favorited"`
	// This user's own rating, 1-10, or null when unrated — never a provider
	// score. Rating is TMDB's community float; the two are different things.
	UserRating *int `json:"user_rating"`
	// The kinds this user holds a copy of the movie on (D-021) — another
	// per-user axis, so, like the flags above, [] on a live untracked view.
	// While a Jellyfin server is configured (D-033) the `jellyfin` entry is
	// the scan's answer rather than the user's, and JellyfinSynced says so:
	// the page renders that tile read-only and the copy routes refuse the
	// kind. Server config on the DTO, exactly as Streaming.Region is.
	Copies         []string `json:"copies"`
	JellyfinSynced bool     `json:"jellyfin_synced"`
	// Where the movie streams, rents or sells in the configured region
	// (D-022). Catalog data, not per-user: it comes out of the same snapshot
	// as the cast, so it renders with TMDB down. Null when the snapshot holds
	// no listing to read; present-but-empty when TMDB lists nobody for that
	// country — two different answers, and the page words them differently.
	Streaming *Streaming `json:"streaming"`
	// TMDB's recommendations for this movie — the "Similar movies" rail
	// (D-024). Catalog data out of the same snapshot as the cast, so it renders
	// with TMDB down. Never null: [] both when the snapshot has no section yet
	// and when TMDB lists nobody, since the rail reads the same either way.
	// Every entry is a foreign title with no local row, reached by tmdb_id.
	Similar []SimilarTitle `json:"similar"`
}

// FetchMovie fetches /movie/{id} and maps the response into a MovieRow without
// touching the database. ID stays 0: the caller decides whether the snapshot
// becomes a catalog row (tracking, refresh) or is served live and discarded.
func (s *Core) FetchMovie(ctx context.Context, tmdbID int64) (MovieRow, error) {
	details, raw, err := s.TMDB.MovieDetails(ctx, tmdbID)
	if err != nil {
		return MovieRow{}, err
	}

	names := make([]string, 0, len(details.Genres))
	for _, g := range details.Genres {
		if g.Name != "" {
			names = append(names, g.Name)
		}
	}
	genresJSON, err := json.Marshal(names)
	if err != nil {
		return MovieRow{}, fmt.Errorf("encoding genres: %w", err)
	}
	genres := string(genresJSON)

	row := MovieRow{
		TMDBID:        details.ID,
		Title:         details.Title,
		OriginalTitle: NilIfEmpty(details.OriginalTitle),
		ReleaseDate:   NilIfEmpty(details.ReleaseDate),
		Overview:      NilIfEmpty(details.Overview),
		Tagline:       NilIfEmpty(details.Tagline),
		GenresJSON:    &genres,
		PosterPath:    NilIfEmpty(details.PosterPath),
		BackdropPath:  NilIfEmpty(details.BackdropPath),
		// Pruned, not verbatim: TMDB's provider section covers every country
		// and one is rendered, so the rest is most of the snapshot for nothing
		// (D-022). Every other section is stored byte for byte.
		MetadataJSON: string(PruneWatchProviders(raw, s.WatchRegion)),
		FetchedAt:    s.Now().UTC().Format(time.RFC3339),
	}
	if details.Runtime > 0 {
		r := details.Runtime
		row.RuntimeMin = &r
	}
	return row, nil
}

// RefreshMovie fetches a fresh snapshot and writes it over the existing catalog
// row (or creates it — tracking's materialise step is the same upsert). The
// snapshot write it performs is the one D-015 sanctions.
func (s *Core) RefreshMovie(ctx context.Context, tmdbID int64) (MovieRow, error) {
	row, err := s.FetchMovie(ctx, tmdbID)
	if err != nil {
		return MovieRow{}, err
	}
	var out MovieRow
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		out, txErr = UpsertMovieByTMDBID(ctx, tx, row)
		return txErr
	})
	return out, err
}

// MediaSnapshot is the shape parsed back out of metadata_json. Only what the
// DTO renders is declared; the snapshot keeps the rest.
//
// Every section it names — credits, videos, images, external ids and the
// scalar facts — appears verbatim on a /tv/{id} body too, so the show details
// view parses into this same type rather than a duplicate. Fields a show has
// no equivalent for (budget, revenue) simply stay zero, which the projection
// already renders as null.
type MediaSnapshot struct {
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int64   `json:"vote_count"`
	Homepage         string  `json:"homepage"`
	OriginalLanguage string  `json:"original_language"`
	Budget           int64   `json:"budget"`
	Revenue          int64   `json:"revenue"`
	Popularity       float64 `json:"popularity"`
	ExternalIDs      struct {
		IMDBID string `json:"imdb_id"`
	} `json:"external_ids"`
	Videos struct {
		Results []SnapshotVideo `json:"results"`
	} `json:"videos"`
	Images struct {
		Backdrops []SnapshotImage `json:"backdrops"`
		Posters   []SnapshotImage `json:"posters"`
	} `json:"images"`
	// The appended recommendations — TMDB's "more like this" list, one page of
	// at most 20 (D-024). A movie body recommends movies, a TV body shows.
	// Stored verbatim like credits and images, projected by SimilarTitles. Not
	// a pointer: the rail renders "no section" and "none listed" the same way,
	// and the missing-sections probes tell the two apart on their own.
	Recommendations struct {
		Results []snapshotRecommendation `json:"results"`
	} `json:"recommendations"`
	Credits struct {
		Cast []SnapshotCastMember `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
	// AggregateCredits is a /tv/{id} body's whole-series cast — TMDB publishes
	// it for shows only, so a movie body never carries the key. A POINTER, like
	// WatchProviders: a show snapshot written before the append was switched
	// from `credits` has no section at all, which is what the show details gate
	// probes for and what ShowCastEntries falls back from.
	AggregateCredits *struct {
		Cast []snapshotAggregateCastMember `json:"cast"`
	} `json:"aggregate_credits"`
	// The appended watch providers, keyed by ISO-3166-1 alpha-2 country
	// (D-022). Every country is stored; the configured region picks one at
	// projection time — see service_streaming.go. A /tv/{id} body carries the
	// same shape, which is why this lives on the shared type.
	// A POINTER, so an absent section is distinguishable from one that lists
	// nobody — see StreamingFrom.
	WatchProviders *snapshotWatchProviders `json:"watch/providers"`
}

type SnapshotCastMember struct {
	ID          int64  `json:"id"` // TMDB person id; 0 when the body has none
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

// snapshotAggregateCastMember is one entry of a show's aggregate_credits cast.
// There is no top-level character: an actor who played several parts across the
// run carries one role per part, each with its own episode count.
type snapshotAggregateCastMember struct {
	ID                int64              `json:"id"` // TMDB person id; 0 when the body has none
	Name              string             `json:"name"`
	ProfilePath       string             `json:"profile_path"`
	Order             int                `json:"order"`
	TotalEpisodeCount int                `json:"total_episode_count"`
	Roles             []snapshotCastRole `json:"roles"`
}

// snapshotCastRole is one part an actor played, with how many episodes it ran.
type snapshotCastRole struct {
	Character    string `json:"character"`
	EpisodeCount int    `json:"episode_count"`
}

type SnapshotVideo struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Type        string `json:"type"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at"` // RFC3339; lexical order is time order
}

// SnapshotImage is one entry of the appended images galleries. VoteAverage and
// VoteCount are TMDB's community score for the artwork itself, not the movie —
// they are what orders a gallery.
type SnapshotImage struct {
	FilePath    string  `json:"file_path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int64   `json:"vote_count"`
}

// TrailerURL picks the video worth linking: YouTube only, a trailer beats a
// teaser, official beats unofficial, then the newest wins — matching what
// TMDB's own page plays. Returns nil when nothing qualifies.
func TrailerURL(videos []SnapshotVideo) *string {
	var best *SnapshotVideo
	for i := range videos {
		v := &videos[i]
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		if v.Type != "Trailer" && v.Type != "Teaser" {
			continue
		}
		if best == nil || trailerRank(v) > trailerRank(best) ||
			(trailerRank(v) == trailerRank(best) && v.PublishedAt > best.PublishedAt) {
			best = v
		}
	}
	if best == nil {
		return nil
	}
	u := "https://www.youtube.com/watch?v=" + url.QueryEscape(best.Key)
	return &u
}

func trailerRank(v *SnapshotVideo) int {
	rank := 0
	if v.Type == "Trailer" {
		rank += 2
	}
	if v.Official {
		rank++
	}
	return rank
}

// RankedVideos filters the snapshot's videos to the usable YouTube entries and
// orders them by trailerRank then recency, capped at MediaGalleryMax. Shared
// verbatim by the Videos tab and the asset sync, so the thumbnails downloaded
// for offline are exactly the ones the page renders.
func RankedVideos(videos []SnapshotVideo) []SnapshotVideo {
	ranked := make([]SnapshotVideo, 0, len(videos))
	for _, v := range videos {
		if v.Site != "YouTube" || v.Key == "" {
			continue
		}
		ranked = append(ranked, v)
	}
	slices.SortStableFunc(ranked, func(a, b SnapshotVideo) int {
		if r := trailerRank(&b) - trailerRank(&a); r != 0 {
			return r
		}
		// RFC3339 is fixed-width, so lexical order is time order; newest first.
		return strings.Compare(b.PublishedAt, a.PublishedAt)
	})

	if len(ranked) > MediaGalleryMax {
		ranked = ranked[:MediaGalleryMax]
	}
	return ranked
}

// movieVideos projects the snapshot's videos into the media section's Videos
// tab. Unlike TrailerURL, which is after the single best trailer, this keeps
// every type TMDB lists — clips and featurettes are the point of a gallery —
// but orders them by the same ranking, so the tab opens on the same video the
// Trailer link plays.
func movieVideos(link AssetLinker, movieID int64, videos []SnapshotVideo) []MovieVideo {
	ranked := RankedVideos(videos)
	out := make([]MovieVideo, 0, len(ranked))
	for _, v := range ranked {
		mv := MovieVideo{
			Name:        v.Name,
			Type:        v.Type,
			Official:    v.Official,
			PublishedAt: NilIfEmpty(v.PublishedAt),
			URL:         "https://www.youtube.com/watch?v=" + url.QueryEscape(v.Key),
		}
		// A key the proxy would reject gets no thumb rather than a URL that
		// 422s — the SPA renders a placeholder tile for null.
		if YouTubeKeyRe.MatchString(v.Key) {
			thumb := link.Pick(
				assets.MovieVideoThumb(movieID, v.Key),
				"api/v1/video/youtube-thumbs/"+v.Key)
			mv.Thumb = &thumb
		}
		out = append(out, mv)
	}
	return out
}

// RankedImages filters and orders one snapshot gallery the way TMDB orders its
// own: best-rated artwork first, votes breaking a tie, file path last so the
// order is stable across refreshes rather than dependent on TMDB's response
// order. Capped at MediaGalleryMax. Shared verbatim by the media tabs and the
// asset sync, so the images downloaded for offline are exactly the ones the
// page renders.
func RankedImages(images []SnapshotImage) []SnapshotImage {
	ordered := make([]SnapshotImage, 0, len(images))
	for _, img := range images {
		if img.FilePath == "" {
			continue
		}
		ordered = append(ordered, img)
	}
	slices.SortStableFunc(ordered, func(a, b SnapshotImage) int {
		if a.VoteAverage != b.VoteAverage {
			if a.VoteAverage > b.VoteAverage {
				return -1
			}
			return 1
		}
		if a.VoteCount != b.VoteCount {
			if a.VoteCount > b.VoteCount {
				return -1
			}
			return 1
		}
		return strings.Compare(a.FilePath, b.FilePath)
	})
	if len(ordered) > MediaGalleryMax {
		ordered = ordered[:MediaGalleryMax]
	}
	return ordered
}

// movieImages projects one snapshot gallery into DTO entries. gallery names
// the asset subdirectory ("backdrops" | "posters") a tracked movie's copies
// live under.
func movieImages(link AssetLinker, movieID int64, gallery string, images []SnapshotImage, sizeThumb, sizeFull string) []MovieImage {
	ordered := RankedImages(images)
	out := make([]MovieImage, 0, len(ordered))
	for _, img := range ordered {
		base := strings.TrimPrefix(img.FilePath, "/")
		out = append(out, MovieImage{
			Thumb: link.Pick(
				assets.MovieGalleryImage(movieID, gallery, base, true),
				"api/v1/video/posters/"+sizeThumb+img.FilePath),
			Full: link.Pick(
				assets.MovieGalleryImage(movieID, gallery, base, false),
				"api/v1/video/posters/"+sizeFull+img.FilePath),
			Width:  img.Width,
			Height: img.Height,
		})
	}
	return out
}

// AssetLinker resolves one catalog item's image slots to a URL: the item's
// permanent copy under data/assets when the file is already on disk, else the
// lazy cache proxy — which is what makes a page render correctly right after
// tracking while the queue is still draining, and for an untracked live view,
// where files is nil and every slot proxies.
//
// It is a value rather than four loose parameters because a show needs the same
// three facts a movie does and threading them through every projection is how
// they drift apart.
// The zero AssetLinker is valid and proxies everything, which is what an
// untracked view — a live show, or a season whose show has no row — gets.
type AssetLinker struct {
	ItemID int64           // movies.id | shows.id; the dest builders need it
	dir    string          // assets.MovieDir(id) | assets.ShowDir(id)
	route  string          // "api/v1/video/movies/12" | "api/v1/video/shows/7"
	files  map[string]bool // nil ⇒ no permanent artwork
	show   bool            // picks the show-shaped dest builders in Poster/Backdrop
}

func MovieLinker(movieID int64, files map[string]bool) AssetLinker {
	return AssetLinker{
		ItemID: movieID,
		dir:    assets.MovieDir(movieID),
		route:  fmt.Sprintf("api/v1/video/movies/%d", movieID),
		files:  files,
	}
}

func ShowLinker(showID int64, files map[string]bool) AssetLinker {
	return AssetLinker{
		ItemID: showID,
		dir:    assets.ShowDir(showID),
		route:  fmt.Sprintf("api/v1/video/shows/%d", showID),
		files:  files,
		show:   true,
	}
}

// pick returns the on-disk URL for dest when it is stored, else proxy.
// Mapping a dest to its serving route is a prefix swap:
// "assets/video/movies/12/poster.jpg" → "api/v1/video/movies/12/assets/poster.jpg".
// Relative, like every asset URL the API returns.
func (l AssetLinker) Pick(dest, proxy string) string {
	if l.files == nil || !l.files[dest] {
		return proxy
	}
	return l.route + "/assets/" + strings.TrimPrefix(dest, l.dir+"/")
}

// Poster resolves the item's poster slot at both sizes — the thumb (w185) and
// the full (w500) — or nil, nil when the row has no poster path. Every card
// and page carries the pair so the SPA can offer both through srcset and let
// the browser pick by screen density and rendered width. Each variant is
// picked on its own, so a half-synced item (thumb on disk, full still queued)
// serves the stored thumb and proxies the full.
func (l AssetLinker) Poster(path *string) (thumb, full *string) {
	if path == nil {
		return nil, nil
	}
	destThumb, destFull := assets.MoviePoster(l.ItemID, true), assets.MoviePoster(l.ItemID, false)
	if l.show {
		destThumb, destFull = assets.ShowPoster(l.ItemID, true), assets.ShowPoster(l.ItemID, false)
	}
	return l.pair(destThumb, destFull, PosterSizeThumb, PosterSizeFull, *path)
}

// Backdrop is Poster for the backdrop slot (w300 / w1280).
func (l AssetLinker) Backdrop(path *string) (thumb, full *string) {
	if path == nil {
		return nil, nil
	}
	destThumb, destFull := assets.MovieBackdrop(l.ItemID, true), assets.MovieBackdrop(l.ItemID, false)
	if l.show {
		destThumb, destFull = assets.ShowBackdrop(l.ItemID, true), assets.ShowBackdrop(l.ItemID, false)
	}
	return l.pair(destThumb, destFull, BackdropSizeThumb, BackdropSizeFull, *path)
}

// SeasonPoster is Poster for one of a SHOW linker's season slots; a movie
// linker has no seasons.
func (l AssetLinker) SeasonPoster(seasonNumber int, path *string) (thumb, full *string) {
	if path == nil {
		return nil, nil
	}
	return l.pair(assets.ShowSeasonPoster(l.ItemID, seasonNumber, true),
		assets.ShowSeasonPoster(l.ItemID, seasonNumber, false),
		PosterSizeThumb, PosterSizeFull, *path)
}

func (l AssetLinker) pair(destThumb, destFull, sizeThumb, sizeFull, path string) (thumb, full *string) {
	t := l.Pick(destThumb, PosterProxyURL(sizeThumb, path))
	f := l.Pick(destFull, PosterProxyURL(sizeFull, path))
	return &t, &f
}

// MovieDetailsFromRow builds the DTO from a stored (or, for a live view,
// in-memory) row. files is the movie's on-disk asset set — nil means "no
// permanent artwork": every image URL falls back to the lazy cache proxy.
// region is the configured watch region (D-022); it picks one country out of
// the snapshot's provider listing and nothing else.
func MovieDetailsFromRow(row MovieRow, files map[string]bool, region string) MovieDetails {
	link := MovieLinker(row.ID, files)
	d := MovieDetails{
		TMDBID:        row.TMDBID,
		Title:         row.Title,
		OriginalTitle: row.OriginalTitle,
		ReleaseDate:   row.ReleaseDate,
		RuntimeMin:    row.RuntimeMin,
		Overview:      row.Overview,
		Tagline:       row.Tagline,
		Genres:        []string{},
		Cast:          []CastMember{},
		Videos:        []MovieVideo{},
		Backdrops:     []MovieImage{},
		Posters:       []MovieImage{},
		FetchedAt:     row.FetchedAt,
		Copies:        []string{},
		Similar:       []SimilarTitle{},
	}
	if row.ID > 0 {
		// 0 means "no catalog row" — a live untracked view renders id null.
		id := row.ID
		d.ID = &id
	}

	if row.ReleaseDate != nil && len(*row.ReleaseDate) >= 4 {
		year := (*row.ReleaseDate)[:4]
		d.Year = &year
	}
	if row.GenresJSON != nil {
		// A corrupt column is not worth failing the page over; the zero value
		// (no genres) is an honest rendering of it.
		_ = json.Unmarshal([]byte(*row.GenresJSON), &d.Genres)
		if d.Genres == nil {
			d.Genres = []string{}
		}
	}
	d.Poster, d.PosterFull = link.Poster(row.PosterPath)

	// Rating, votes, credits, links and facts live only in the snapshot. Same
	// tolerance as genres: an undecodable snapshot renders as "no credits".
	var snap MediaSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)

	if snap.VoteAverage > 0 {
		v := snap.VoteAverage
		d.Rating = &v
	}
	if snap.VoteCount > 0 {
		n := snap.VoteCount
		d.Votes = &n
	}
	d.Homepage = NilIfEmpty(snap.Homepage)
	d.IMDBID = NilIfEmpty(snap.ExternalIDs.IMDBID)
	d.TrailerURL = TrailerURL(snap.Videos.Results)
	d.OriginalLanguage = NilIfEmpty(snap.OriginalLanguage)
	// TMDB reports 0 for an unknown budget/revenue and for a movie with no
	// popularity data — null is the honest rendering of all three.
	if snap.Budget > 0 {
		d.Budget = &snap.Budget
	}
	if snap.Revenue > 0 {
		d.Revenue = &snap.Revenue
	}
	if snap.Popularity > 0 {
		d.Popularity = &snap.Popularity
	}
	for _, member := range snap.Credits.Crew {
		if member.Job == "Director" && member.Name != "" {
			name := member.Name
			d.Director = &name
			break
		}
	}

	cast := snap.Credits.Cast
	slices.SortStableFunc(cast, func(a, b SnapshotCastMember) int {
		return a.Order - b.Order
	})
	for _, member := range cast {
		if member.Name == "" {
			continue
		}
		cm := CastMember{TMDBID: PersonIDOf(member.ID), Name: member.Name, Character: NilIfEmpty(member.Character)}
		if member.ProfilePath != "" {
			photo := link.Pick(
				assets.MovieCastPhoto(row.ID, strings.TrimPrefix(member.ProfilePath, "/")),
				"api/v1/video/posters/"+PosterSizeThumb+member.ProfilePath)
			cm.Photo = &photo
		}
		d.Cast = append(d.Cast, cm)
	}

	d.Streaming = StreamingFrom(snap, region)
	d.Similar = SimilarTitles(snap)
	d.Videos = movieVideos(link, row.ID, snap.Videos.Results)
	d.Backdrops = movieImages(link, row.ID, "backdrops", snap.Images.Backdrops, BackdropSizeThumb, BackdropSizeFull)
	d.Posters = movieImages(link, row.ID, "posters", snap.Images.Posters, PosterSizeThumb, PosterSizeFull)
	return d
}

// PersonIDOf turns a body's person id into the DTO's nullable form: 0 (the
// zero value of a body without one) is null, not a person.
func PersonIDOf(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

func NilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
