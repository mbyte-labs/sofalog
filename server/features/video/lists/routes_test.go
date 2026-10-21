package lists

import (
	"net/http"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/copies"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/discover"
	"mbyte.ro/sofalog/features/video/history"
	"mbyte.ro/sofalog/features/video/jellyfin"
	"mbyte.ro/sofalog/features/video/media"
	"mbyte.ro/sofalog/features/video/people"
	"mbyte.ro/sofalog/features/video/ratings"
	"mbyte.ro/sofalog/features/video/restore"
	"mbyte.ro/sofalog/features/video/stats"
	"mbyte.ro/sofalog/features/video/tmdbaccount"
	"mbyte.ro/sofalog/features/video/upcoming"
	"mbyte.ro/sofalog/features/video/upnext"
	"mbyte.ro/sofalog/features/video/watches"
)

// allRoutes registers every video feature's routes around this package's
// Service, the way the root wiring does, so a handler test can reach the
// routes it exercises in other features too.
type allRoutes struct {
	svc    *Service
	cookie common.CookieSettings
}

func allHandlers(svc *Service, cookie common.CookieSettings) allRoutes {
	return allRoutes{svc: svc, cookie: cookie}
}

func (a allRoutes) RegisterRoutes(mux *http.ServeMux) {
	upnext.NewHandler(upnext.NewService(a.svc.Core)).RegisterRoutes(mux)
	upcoming.NewHandler(upcoming.NewService(a.svc.Core)).RegisterRoutes(mux)
	history.NewHandler(history.NewService(a.svc.Core)).RegisterRoutes(mux)
	discover.NewHandler(discover.NewService(a.svc.Core)).RegisterRoutes(mux)
	details.NewHandler(details.NewService(a.svc.Core)).RegisterRoutes(mux)
	people.NewHandler(people.NewService(a.svc.Core)).RegisterRoutes(mux)
	media.NewHandler(media.NewService(a.svc.Core)).RegisterRoutes(mux)
	watches.NewHandler(watches.NewService(a.svc.Core)).RegisterRoutes(mux)
	NewHandler(a.svc).RegisterRoutes(mux)
	copies.NewHandler(copies.NewService(a.svc.Core)).RegisterRoutes(mux)
	ratings.NewHandler(ratings.NewService(a.svc.Core)).RegisterRoutes(mux)
	restore.NewHandler(restore.NewService(a.svc.Core)).RegisterRoutes(mux)
	stats.NewHandler(stats.NewService(a.svc.Core)).RegisterRoutes(mux)
	tmdbaccount.NewHandler(tmdbaccount.NewService(a.svc.Core), a.cookie).RegisterRoutes(mux)
	jellyfin.NewHandler(jellyfin.NewService(a.svc.Core)).RegisterRoutes(mux)
}
