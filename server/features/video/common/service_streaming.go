package common

import (
	"cmp"
	"encoding/json"
	"slices"
)

// "Where to watch" — TMDB's watch-provider data, sourced from JustWatch
// (D-022).
//
// Three things make this cheap. It rides the /movie/{id} and /tv/{id} calls
// through append_to_response, so it is not a sixth provider call site. The
// section TMDB returns covers EVERY country — 111 of them for one film, some
// 45% of the whole response — so PruneWatchProviders keeps only the configured
// region before the body is stored, and the marker it leaves behind is what
// makes a change of region self-heal. And a provider logo is a TMDB image path
// like any other, so it goes through the existing poster proxy into
// data/cache — never into data/assets, which holds an item's own artwork and
// whose reconciler deletes anything its desired set does not name.
//
// Seasons and episodes have no listing of their own: TMDB publishes providers
// per movie and per show only, so those two views borrow the owning show's.

// StreamingMax bounds the tile row. A popular title in a large market lists
// twenty-odd services; a row that long stops being an answer to "where can I
// watch this". Presentation only — nothing stored is truncated.
const StreamingMax = 12

// The offer buckets TMDB returns, strongest first. "Strongest" means cheapest
// to act on: something already included in a subscription beats something that
// must be bought. The order is also the tile order and the dedupe precedence,
// so a service listed under both rent and buy shows up once, as Rent.
var streamingOffers = []struct {
	offer string
	pick  func(snapshotWatchRegion) []snapshotWatchProvider
}{
	{"stream", func(r snapshotWatchRegion) []snapshotWatchProvider { return r.Flatrate }},
	{"free", func(r snapshotWatchRegion) []snapshotWatchProvider { return r.Free }},
	{"ads", func(r snapshotWatchRegion) []snapshotWatchProvider { return r.Ads }},
	{"rent", func(r snapshotWatchRegion) []snapshotWatchProvider { return r.Rent }},
	{"buy", func(r snapshotWatchRegion) []snapshotWatchProvider { return r.Buy }},
}

// snapshotWatchProviders is the appended section as read back out of a stored
// snapshot. SofalogRegion is the marker PruneWatchProviders writes; it is empty
// on a body stored before pruning existed, which is a legitimate state — such a
// body carries every country, so any region can still be projected out of it.
type snapshotWatchProviders struct {
	SofalogRegion string                         `json:"sofalog_region"`
	Results       map[string]snapshotWatchRegion `json:"results"`
}

// snapshotWatchRegion is one country's listing inside the appended section.
// Link is JustWatch's page for this title in that country — the one outbound
// link the section carries, and what TMDB's terms ask be credited.
type snapshotWatchRegion struct {
	Link     string                  `json:"link"`
	Flatrate []snapshotWatchProvider `json:"flatrate"`
	Free     []snapshotWatchProvider `json:"free"`
	Ads      []snapshotWatchProvider `json:"ads"`
	Rent     []snapshotWatchProvider `json:"rent"`
	Buy      []snapshotWatchProvider `json:"buy"`
}

type snapshotWatchProvider struct {
	ProviderID      int64  `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	LogoPath        string `json:"logo_path"` // "/abc.jpg", or "" when absent
	DisplayPriority int    `json:"display_priority"`
}

// StreamingProvider is one tile of the details pages' "Watch now" section.
type StreamingProvider struct {
	// ProviderID is TMDB's stable id for the service. It addresses nothing
	// here — it is the render key, which a name is not: two entries can share
	// a display name across regions.
	ProviderID int64  `json:"provider_id"`
	Name       string `json:"name"`
	// Offer is how the title is available: stream | free | ads | rent | buy.
	// One value, the strongest — see streamingOffers.
	Offer string `json:"offer"`
	// Logo is the relative URL of the locally proxied mark, or null when TMDB
	// has none (the SPA falls back to a monogram). Never a provider URL.
	Logo *string `json:"logo"`
}

// Streaming is the whole section for one item: who carries it, in one country.
// It is served as a POINTER, because "we have no answer" and "the answer is
// nothing" are different facts and the page says different things about them:
//
//   - null — no listing available at all: the snapshot predates the appended
//     section, or the item is a season/episode of an untracked show. The page
//     stays quiet; claiming unavailability here would be a guess.
//   - Providers: [] — TMDB has an answer for this country and it is empty. The
//     page says so out loud.
//   - Providers: [...] — listed.
//
// Region is always set, which is what lets the empty case name the country.
type Streaming struct {
	Region string `json:"region"`
	// Link is JustWatch's page for this title in Region, or null when the
	// snapshot has none.
	Link      *string             `json:"link"`
	Providers []StreamingProvider `json:"providers"` // never null; [] when none
}

// knownEmptyStreaming is the middle state above: the snapshot carries a
// listing for this country and it names nobody. Distinct from a nil return,
// which means the snapshot has no listing to read at all.
func knownEmptyStreaming(region string) *Streaming {
	return &Streaming{Region: region, Providers: []StreamingProvider{}}
}

// StreamingFrom projects one region's listing out of a stored snapshot. It is
// the only reader of the appended section, shared by all four details views so
// a movie, a show, its season and its episode can never disagree about what
// the same snapshot says.
//
// nil means the snapshot has no section to read — an old row whose refresh has
// not happened or has failed. A snapshot that HAS the section but nothing for
// this country comes back non-nil and empty: that is an answer, and the page
// renders it as one.
func StreamingFrom(snap MediaSnapshot, region string) *Streaming {
	wp := snap.WatchProviders
	if wp == nil {
		return nil
	}
	// Pruned to some OTHER country: we hold no answer for this one. Reachable
	// only when the region changed and the refresh the gate asked for failed,
	// and reporting "nothing here" then would be a lie rather than an answer.
	// An empty marker is the pre-pruning shape, which carries every country.
	if wp.SofalogRegion != "" && wp.SofalogRegion != region {
		return nil
	}
	out := knownEmptyStreaming(region)
	listing, ok := wp.Results[region]
	if !ok {
		return out
	}
	if listing.Link != "" {
		link := listing.Link
		out.Link = &link
	}

	// First bucket wins: streamingOffers is in precedence order, so a service
	// under both rent and buy is one tile, labelled Rent.
	seen := make(map[int64]bool)
	for _, bucket := range streamingOffers {
		for _, p := range bucket.pick(listing) {
			if p.ProviderName == "" || seen[p.ProviderID] {
				continue
			}
			seen[p.ProviderID] = true
			tile := StreamingProvider{
				ProviderID: p.ProviderID,
				Name:       p.ProviderName,
				Offer:      bucket.offer,
			}
			if p.LogoPath != "" {
				// w185 is already in the proxy whitelist, and a provider mark
				// is small — one variant is all this renders, like the
				// achievement icons and video thumbnails.
				u := PosterProxyURL(PosterSizeThumb, p.LogoPath)
				tile.Logo = &u
			}
			out.Providers = append(out.Providers, tile)
		}
	}

	// display_priority is TMDB's own per-bucket ordering, so it only orders
	// within an offer; the name breaks the remaining ties so the row is stable
	// across renders of the same snapshot.
	priority := make(map[int64]int, len(seen))
	for _, bucket := range streamingOffers {
		for _, p := range bucket.pick(listing) {
			if _, ok := priority[p.ProviderID]; !ok {
				priority[p.ProviderID] = p.DisplayPriority
			}
		}
	}
	slices.SortStableFunc(out.Providers, func(a, b StreamingProvider) int {
		if r := cmp.Compare(offerRank(a.Offer), offerRank(b.Offer)); r != 0 {
			return r
		}
		if r := cmp.Compare(priority[a.ProviderID], priority[b.ProviderID]); r != 0 {
			return r
		}
		return cmp.Compare(a.Name, b.Name)
	})

	if len(out.Providers) > StreamingMax {
		out.Providers = out.Providers[:StreamingMax]
	}
	return out
}

// offerRank is an offer's index in streamingOffers — its display precedence.
// An unknown value sorts last rather than first, so a bucket TMDB adds later
// lands at the end of the row instead of ahead of the subscriptions.
func offerRank(offer string) int {
	for i, b := range streamingOffers {
		if b.offer == offer {
			return i
		}
	}
	return len(streamingOffers)
}

// WatchProvidersKey is the appended section's name in a TMDB body. It has a
// slash in it, which is TMDB's spelling, not a path.
const WatchProvidersKey = "watch/providers"

// PrunedWatchProviders is what gets STORED in place of TMDB's section: the one
// country the server is configured for, plus the marker saying which. It is
// deliberately shaped so `results` still keys by region — MediaSnapshot and
// StreamingFrom read a pruned snapshot and a raw TMDB body identically.
type PrunedWatchProviders struct {
	// SofalogRegion is ours, not TMDB's, and is the whole point: an empty
	// `results` is ambiguous on its own ("nothing listed here" and "pruned to
	// some other country" look alike), and without it a change of
	// TMDB_WATCH_REGION would silently render nothing until each item aged
	// out of its 48-hour window.
	SofalogRegion string                     `json:"sofalog_region"`
	Results       map[string]json.RawMessage `json:"results"`
}

// PruneWatchProviders rewrites a fetched body so it carries only `region`'s
// listing. Storing all 111 countries to render one is most of the snapshot for
// nothing; the cost of pruning is that the stored body is no longer TMDB's
// verbatim, and that changing the region needs a refetch — which the marker
// above turns into one automatic refresh per item on its next view.
//
// Only the top level is re-serialized: every other section stays the bytes
// TMDB sent. A body that cannot be parsed, or carries no section at all, is
// returned untouched — the projection already renders such a snapshot as
// empty, and the probe will ask for a refresh.
func PruneWatchProviders(raw json.RawMessage, region string) json.RawMessage {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	section, ok := body[WatchProvidersKey]
	if !ok {
		return raw
	}
	var parsed struct {
		Results map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(section, &parsed); err != nil {
		return raw
	}

	kept := PrunedWatchProviders{SofalogRegion: region, Results: map[string]json.RawMessage{}}
	if listing, ok := parsed.Results[region]; ok {
		kept.Results[region] = listing
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return raw
	}
	body[WatchProvidersKey] = encoded

	out, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return out
}
