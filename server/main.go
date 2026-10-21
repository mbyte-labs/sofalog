// sofalog — self-hosted multi-user media tracker. One binary: Go API,
// embedded Svelte SPA, SQLite.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/auth"
	"mbyte.ro/sofalog/config"
	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/migrations"
	"mbyte.ro/sofalog/provider"
	"mbyte.ro/sofalog/tz"
	"mbyte.ro/sofalog/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// describeInterval renders a refresh interval for the startup log: "disabled"
// for zero, otherwise the duration.
func describeInterval(d time.Duration) string {
	if d == 0 {
		return "disabled"
	}
	return d.String()
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration:\n%w", err)
	}
	if cfg.DatabaseURL != "" {
		return errors.New("DATABASE_URL (Postgres) is not supported yet — unset it to use SQLite")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	database, err := db.Open(ctx, cfg.SQLitePath())
	if err != nil {
		return err
	}
	defer database.Close()

	applied, err := db.Migrate(ctx, database, migrations.FS())
	if err != nil {
		return err
	}
	slog.Info("migrations applied", "count", applied)

	// TMDB is checked, never required. The whole point of the local-first design
	// is that an unreachable or unconfigured provider costs you the ability to add
	// new items, not the ability to use the library you already have — so every
	// failure below is a warning and startup continues.
	tmdb := provider.NewTMDB(provider.TMDBCredentials{
		APIKeyV3: cfg.TMDBAPIKeyV3,
		APIKeyV4: cfg.TMDBAPIKeyV4,
	})
	for _, problem := range tmdb.CredentialWarnings() {
		slog.Warn("tmdb: ignoring a misplaced credential", "problem", problem)
	}
	if !tmdb.Configured() {
		slog.Warn("tmdb: no credential set — adding movies and shows will fail; browsing is unaffected",
			"expected", "TMDB_API_KEY_V3 or TMDB_API_KEY_V4")
	} else {
		// Fetching /configuration once at startup caches the image base URL TMDB
		// documents as changeable, and doubles as a credential check: a bad key
		// shows up in the log now rather than on a user's first search.
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		imageCfg, err := tmdb.Configuration(cctx)
		cancel()
		if err != nil {
			slog.Warn("tmdb: configuration fetch failed, will retry on first use",
				"auth", tmdb.AuthMode(), "err", err)
		} else {
			slog.Info("tmdb: ready", "image_base", imageCfg.SecureBaseURL, "auth", tmdb.AuthMode())
		}
	}

	// The asset pipeline: permanent artwork of tracked items lives in
	// data/assets, filled by the queue worker draining asset_jobs. Interrupted
	// downloads are re-queued at startup (crash recovery), then the worker runs
	// until shutdown. It only downloads what tracking and refreshes enqueue.
	cache := assets.NewCache(cfg.DataDir)
	tree := assets.NewTree(cfg.DataDir)
	youtube := provider.NewYouTube()
	queue := assets.NewQueue(database, tree, cache, map[string]assets.Fetcher{
		"tmdb": func(ctx context.Context, sourceURL string) ([]byte, error) {
			size, file, ok := strings.Cut(sourceURL, "/")
			if !ok {
				return nil, fmt.Errorf("malformed tmdb source %q", sourceURL)
			}
			return tmdb.DownloadImage(ctx, size, "/"+file)
		},
		"youtube": func(ctx context.Context, key string) ([]byte, error) {
			return youtube.Thumbnail(ctx, key)
		},
	})
	if err := assets.ResetRunning(ctx, database); err != nil {
		return fmt.Errorf("re-queueing interrupted asset downloads: %w", err)
	}
	go queue.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// The household's Jellyfin server (D-033). Like TMDB it is checked, never
	// required: an unreachable server costs the jellyfin copy tile its
	// freshness, nothing else, so the probe is a warning and startup goes on.
	jellyfin := provider.NewJellyfin(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey)
	if jellyfin.Configured() {
		pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		info, err := jellyfin.Ping(pctx)
		cancel()
		if err != nil {
			slog.Warn("jellyfin: server probe failed, the scan will retry on its schedule",
				"url", cfg.Jellyfin.URL, "err", err)
		} else {
			slog.Info("jellyfin: ready", "url", cfg.Jellyfin.URL, "server", info.Name, "version", info.Version,
				"scan", describeInterval(cfg.JellyfinScanInterval))
		}
	}
	// Config already validated the name; loading it again cannot fail.
	defaultZone, err := tz.Load(cfg.DefaultTimezone)
	if err != nil {
		return fmt.Errorf("DEFAULT_TIMEZONE: %w", err)
	}
	slog.Info("time zone: default for users who have not chosen one", "zone", cfg.DefaultTimezone)
	videoSvc := video.NewService(database, tmdb, cache, youtube, tree, queue, cfg.TMDBWatchRegion,
		cfg.TMDBRefreshInterval, cfg.TMDBArchiveRefreshInterval, cfg.TMDBLinkRedirectURI(),
		jellyfin, cfg.JellyfinScanInterval, defaultZone)
	// The video feature's background work: the scheduled TMDB refresh (D-006,
	// D-025), the TMDB rating outbox worker (D-029), the Jellyfin library scan
	// (D-033) and any backup restore running past its request. Run starts the
	// first three and stops all four at shutdown.
	slog.Info("tmdb refresh: schedule",
		"active", describeInterval(cfg.TMDBRefreshInterval),
		"archive", describeInterval(cfg.TMDBArchiveRefreshInterval))
	if tmdb.Configured() {
		// The link's callback is fixed by BASE_URL; stated once so a wrong
		// BASE_URL shows up here rather than on a user's first Link.
		slog.Info("tmdb link: account linking ready", "redirect_uri", cfg.TMDBLinkRedirectURI())
	}
	go videoSvc.Run(ctx)
	video.NewHandler(videoSvc, video.CookieSettings{Path: cfg.BasePath(), Secure: cfg.CookieSecure}).RegisterRoutes(mux)
	// Single sign-on. Configuring a provider disables local authentication
	// entirely (D-018), so a nil oidc here is what keeps passwords working.
	var oidcClient *auth.OIDC
	if cfg.OIDC.Enabled {
		oidcClient = auth.NewOIDC(auth.OIDCSettings{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURI:  cfg.OIDCRedirectURI(),
			Scopes:       cfg.OIDC.Scopes,
			AdminClaim:   cfg.OIDC.AdminClaim,
			AdminValue:   cfg.OIDC.AdminValue,
			ProviderName: cfg.OIDC.ProviderName,
		})
		// Pasting the wrong callback into the IdP is the most common setup
		// mistake, so state the exact value once at startup.
		slog.Info("oidc: local authentication is disabled; single sign-on only",
			"issuer", cfg.OIDC.Issuer, "redirect_uri", cfg.OIDCRedirectURI())
		if cfg.OIDC.AdminClaim == "" {
			slog.Warn("oidc: OIDC_ADMIN_CLAIM/OIDC_ADMIN_VALUE are unset — no one will be granted admin")
		}
		// Discovery is attempted now for a fast, clear failure, but is not
		// fatal: an IdP in the same compose stack often starts after this
		// app, and crash-looping on that is worse than a retry on first use.
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := oidcClient.Discover(dctx)
		cancel()
		if err != nil {
			slog.Warn("oidc: discovery failed, will retry on first sign-in", "err", err)
		} else {
			slog.Info("oidc: ready", "provider", cfg.OIDC.ProviderName)
		}
	}

	authSvc := auth.NewService(database, oidcClient, cfg.DefaultTimezone)
	authCookie := auth.CookieConfig{Path: cfg.BasePath(), Secure: cfg.CookieSecure}
	auth.NewHandler(authSvc, authCookie).RegisterRoutes(mux)
	mux.Handle("/", web.Handler(cfg.BasePath()))

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpx.Chain(mux,
			httpx.RequestID,
			httpx.RealIP(cfg.TrustedProxies),
			httpx.Logging,
			httpx.Recover,
			// Innermost: needs RealIP resolved (sessions record the client
			// ip) and Recover outside it.
			auth.Sessions(authSvc, authCookie),
		),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr, "base_url", cfg.BaseURL.String())
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
