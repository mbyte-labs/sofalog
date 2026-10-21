package details

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// EpisodeDetailsParams carries the raw query strings of GET
// /video/episodes/details; the service owns validation. The season is
// addressed exactly as the season page addresses it — SeasonID alone, or
// TMDBID and Season together — plus the episode number, which both forms need.
type EpisodeDetailsParams struct {
	SeasonID string // local seasons.id
	TMDBID   string // the SHOW's tmdb id
	Season   string // season number; 0 is Specials
	Episode  string // episode number within the season
}

// EpisodeDetails is the episode page's DTO.
//
// There is no id for the episode itself in this shape and no tmdb_id either: an
// episode rides its season's snapshot, so it is addressed by its season plus
// its number. Its own local id, watched flag and rating travel inside Episode.
//
// The season and show context is denormalised in so the page renders its
// header and its way back up without a second call.
type EpisodeDetails struct {
	// SeasonID is null for the same reason SeasonDetails.ID is: the season has
	// no catalog row until its show is tracked (D-015), and the page then links
	// back up by (show_tmdb_id, season_number) instead.
	SeasonID     *int64 `json:"season_id"`
	SeasonNumber int    `json:"season_number"` // 0 is Specials
	SeasonName   string `json:"season_name"`
	ShowTMDBID   int64  `json:"show_tmdb_id"`
	ShowTitle    string `json:"show_title"`
	// The same shape the season page's episode list is made of, so one
	// projection serves both views.
	Episode SeasonEpisode `json:"episode"`
	// FetchedAt is the SEASON snapshot's time — an episode has no snapshot of
	// its own, it is one entry in its season's.
	FetchedAt string `json:"fetched_at"` // RFC3339 UTC
	// Cast is the SEASON's regular cast — TMDB's `credits` for the season, the
	// same people on every episode of it, which is what TMDB's own episode
	// pages list as an episode's cast. GuestStars is the episode-specific half:
	// who was credited for this one episode beyond the regulars. Both in
	// billing order, both always present ([] when TMDB lists nobody), both with
	// `episodes` null — neither section carries counts. Photos resolve through
	// the show's cast directory like the season page's.
	Cast       []common.CastMember `json:"cast"`
	GuestStars []common.CastMember `json:"guest_stars"`
	// SeasonCopies is the kinds this user holds a copy of the OWNING SEASON on
	// (D-021), borrowed for display: an episode is never itself owned, so the
	// page shows these read-only and sends the user to the season page to
	// change them. Named season_copies for the same reason the fields above are
	// season_-prefixed — a bare "copies" would read as the episode's own.
	SeasonCopies []string `json:"season_copies"`
	// JellyfinSynced is the season's, borrowed with SeasonCopies (D-033): the
	// tile was already read-only here, this only changes its wording.
	JellyfinSynced bool `json:"jellyfin_synced"`
	// Where to watch, borrowed from the show two rungs up (D-022) — the same
	// listing the season page shows, projected from the same call so the two
	// can never disagree. Null while the show is untracked.
	Streaming *common.Streaming `json:"streaming"`
	// Whether this user has dropped the show two rungs up (D-010), borrowed the
	// same way Streaming is and read out of the same season call, so the season
	// and episode pages can never disagree about it. See
	// SeasonDetails.ShowDropped for why it is a bool and not the status string.
	ShowDropped bool `json:"show_dropped"`
}

// EpisodeDetails returns one episode, projected out of its season's snapshot.
//
// It stores nothing an episode-shaped view of its own would store: the season
// row is resolved by resolveSeasonRow, whose D-015 contract this inherits
// whole — a first view by tmdb key materialises the season, a stale one
// refreshes, a refresh failure stale-serves, and a season_id with no row is a
// plain 404 with no TMDB call. So a 503 is reachable only when the season has
// never been viewed AND TMDB is unreachable; a season with a row never 503s.
//
// An episode number the season does not list is a 404 — the season resolved,
// the episode does not exist.
//
// userID carries through to the projected episode's id and watched flag, so
// the page's mark control renders truthfully — an untracked show yields a null
// id and watched=false, and the mark call addresses the episode by the show's
// tmdb_id plus the two numbers instead.
func (s *Service) EpisodeDetails(ctx context.Context, userID int64, p EpisodeDetailsParams) (EpisodeDetails, error) {
	if p.Episode == "" {
		return EpisodeDetails{}, fmt.Errorf("%w: episode is required", httpx.ErrValidation)
	}
	// 0 is a real episode number — some shows number a pilot or a recap 0 — so
	// only negatives are invalid.
	episodeNumber, err := strconv.Atoi(p.Episode)
	if err != nil || episodeNumber < 0 {
		return EpisodeDetails{}, fmt.Errorf("%w: episode must be a non-negative integer", httpx.ErrValidation)
	}

	row, err := s.resolveSeasonRow(ctx, p.SeasonID, p.TMDBID, p.Season)
	if err != nil {
		return EpisodeDetails{}, err
	}

	link, state, streaming, err := s.seasonRenderState(ctx, userID, row)
	if err != nil {
		return EpisodeDetails{}, err
	}
	season := seasonDetailsFromRow(row, link, state, streaming, s.JellyfinSynced())
	for _, ep := range season.Episodes {
		if ep.EpisodeNumber != episodeNumber {
			continue
		}
		return EpisodeDetails{
			SeasonID:       season.ID,
			SeasonNumber:   season.SeasonNumber,
			SeasonName:     season.Name,
			ShowTMDBID:     season.ShowTMDBID,
			ShowTitle:      season.ShowTitle,
			Episode:        ep,
			Cast:           seasonRegularCast(row.MetadataJSON, link),
			GuestStars:     episodeGuestStars(row.MetadataJSON, episodeNumber, link),
			FetchedAt:      season.FetchedAt,
			SeasonCopies:   season.Copies,
			JellyfinSynced: season.JellyfinSynced,
			Streaming:      season.Streaming,
			ShowDropped:    season.ShowDropped,
		}, nil
	}
	return EpisodeDetails{}, fmt.Errorf("%w: no such episode in this season", httpx.ErrNotFound)
}

// seasonRegularCast reads the season document's folded `credits` section — the
// season's regular cast — through the plain-credits projection. Tolerant like
// every snapshot read: a corrupt or pre-append document renders as no cast.
func seasonRegularCast(metadataJSON string, link common.AssetLinker) []common.CastMember {
	var media common.MediaSnapshot
	_ = json.Unmarshal([]byte(metadataJSON), &media)
	return castMembers(common.LegacyCastEntries(media.Credits.Cast), link)
}

// episodeGuestStars reads one episode's guest_stars out of the season document.
// They arrive in the plain credits shape, so the same projection serves them.
func episodeGuestStars(metadataJSON string, episodeNumber int, link common.AssetLinker) []common.CastMember {
	var snap common.SeasonSnapshot
	_ = json.Unmarshal([]byte(metadataJSON), &snap)
	var guests []provider.TMDBCastCredit
	for _, e := range snap.Episodes {
		if e.EpisodeNumber == episodeNumber {
			guests = e.GuestStars
			break
		}
	}
	cast := make([]common.SnapshotCastMember, 0, len(guests))
	for _, g := range guests {
		cast = append(cast, common.SnapshotCastMember{
			ID: g.ID, Name: g.Name, Character: g.Character, ProfilePath: g.ProfilePath, Order: g.Order,
		})
	}
	return castMembers(common.LegacyCastEntries(cast), link)
}
