// Package video wires the video component (movies + shows). Each feature
// lives in its own sub-package and imports only video/common, never a
// sibling (D-037); this package builds the shared common.Core, every
// feature's Service, registers their routes and runs their background work.
package video

import (
	"context"
	"net/http"
	"sync"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/copies"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/discover"
	"mbyte.ro/sofalog/features/video/history"
	"mbyte.ro/sofalog/features/video/jellyfin"
	"mbyte.ro/sofalog/features/video/lists"
	"mbyte.ro/sofalog/features/video/media"
	"mbyte.ro/sofalog/features/video/people"
	"mbyte.ro/sofalog/features/video/ratings"
	"mbyte.ro/sofalog/features/video/refresh"
	"mbyte.ro/sofalog/features/video/restore"
	"mbyte.ro/sofalog/features/video/stats"
	"mbyte.ro/sofalog/features/video/tmdbaccount"
	"mbyte.ro/sofalog/features/video/upcoming"
	"mbyte.ro/sofalog/features/video/upnext"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/provider"
)

// CookieSettings is what the TMDB link flow's cookie carries (D-029).
type CookieSettings = common.CookieSettings

// Service is the whole video component: one shared core and every feature.
type Service struct {
	core        *common.Core
	upnext      *upnext.Service
	upcoming    *upcoming.Service
	history     *history.Service
	watches     *watches.Service
	ratings     *ratings.Service
	lists       *lists.Service
	copies      *copies.Service
	jellyfin    *jellyfin.Service
	details     *details.Service
	people      *people.Service
	discover    *discover.Service
	media       *media.Service
	restore     *restore.Service
	stats       *stats.Service
	tmdbaccount *tmdbaccount.Service
	refresh     *refresh.Service
}

// NewService wires the video component. refreshEvery and archiveEvery are the
// two scheduled refresh jobs' intervals (D-025); zero disables the job.
// tmdbLinkRedirectURI is the TMDB account link's callback URL (D-029).
// jellyfinClient is the library scan's client and jellyfinEvery its cadence
// (D-033); an unconfigured client disables the scan entirely, a zero cadence
// leaves only the settings page's "scan now".
func NewService(database *db.DB, tmdb *provider.TMDB, cache *assets.Cache, youtube *provider.YouTube, tree *assets.Tree, queue *assets.Queue, watchRegion string, refreshEvery, archiveEvery time.Duration, tmdbLinkRedirectURI string, jellyfinClient *provider.Jellyfin, jellyfinEvery time.Duration, defaultZone *time.Location) *Service {
	c := common.NewCore(database, tmdb, cache, youtube, tree, queue, watchRegion, refreshEvery, archiveEvery, tmdbLinkRedirectURI, jellyfinClient, jellyfinEvery)
	c.DefaultZone = defaultZone
	return &Service{
		core:        c,
		upnext:      upnext.NewService(c),
		upcoming:    upcoming.NewService(c),
		history:     history.NewService(c),
		watches:     watches.NewService(c),
		ratings:     ratings.NewService(c),
		lists:       lists.NewService(c),
		copies:      copies.NewService(c),
		jellyfin:    jellyfin.NewService(c),
		details:     details.NewService(c),
		people:      people.NewService(c),
		discover:    discover.NewService(c),
		media:       media.NewService(c),
		restore:     restore.NewService(c),
		stats:       stats.NewService(c),
		tmdbaccount: tmdbaccount.NewService(c),
		refresh:     refresh.NewService(c),
	}
}

// Run owns the component's long-lived goroutines: the scheduled refresh
// (refresh), the TMDB outbox worker and setup pulls (tmdbaccount), the
// Jellyfin library scan (jellyfin) and backup restores running past their
// request (restore). Each feature's Run blocks until ctx ends and its own
// work has stopped; Run returns once all of them have.
func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, f := range []interface{ Run(context.Context) }{s.refresh, s.tmdbaccount, s.jellyfin, s.restore} {
		wg.Go(func() { f.Run(ctx) })
	}
	wg.Wait()
}

type Handler struct {
	svc *Service
	// cookie is what the TMDB link flow's cookie carries (D-029): BASE_URL's
	// path and Secure flag, handed in from main.
	cookie CookieSettings
}

func NewHandler(svc *Service, cookie CookieSettings) *Handler {
	return &Handler{svc: svc, cookie: cookie}
}

// RegisterRoutes registers every video feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	upnext.NewHandler(h.svc.upnext).RegisterRoutes(mux)
	upcoming.NewHandler(h.svc.upcoming).RegisterRoutes(mux)
	history.NewHandler(h.svc.history).RegisterRoutes(mux)
	discover.NewHandler(h.svc.discover).RegisterRoutes(mux)
	details.NewHandler(h.svc.details).RegisterRoutes(mux)
	people.NewHandler(h.svc.people).RegisterRoutes(mux)
	media.NewHandler(h.svc.media).RegisterRoutes(mux)
	watches.NewHandler(h.svc.watches).RegisterRoutes(mux)
	lists.NewHandler(h.svc.lists).RegisterRoutes(mux)
	copies.NewHandler(h.svc.copies).RegisterRoutes(mux)
	ratings.NewHandler(h.svc.ratings).RegisterRoutes(mux)
	restore.NewHandler(h.svc.restore).RegisterRoutes(mux)
	stats.NewHandler(h.svc.stats).RegisterRoutes(mux)
	tmdbaccount.NewHandler(h.svc.tmdbaccount, h.cookie).RegisterRoutes(mux)
	jellyfin.NewHandler(h.svc.jellyfin).RegisterRoutes(mux)
}
