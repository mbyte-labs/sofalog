// Package config parses and validates all environment configuration.
// It is the only package in the module that reads the environment.
//
// Values come from the process environment, layered over an optional .env file
// (see readEnvFile). A real environment variable always wins over the file, so a
// container or systemd unit overrides a stale local .env rather than the reverse.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"mbyte.ro/sofalog/tz"
)

// watchRegionRe matches an ISO-3166-1 alpha-2 country code. Membership of the
// real list is TMDB's business — an unknown-but-well-formed code simply lists
// no providers, exactly as a country TMDB has no data for does.
var watchRegionRe = regexp.MustCompile(`^[A-Z]{2}$`)

// The two scheduled TMDB refresh jobs' defaults (D-025). The active job covers
// what people are currently interested in; the archive job covers what they
// have finished with, where only artwork and streaming listings still move.
const (
	defaultRefreshInterval        = 7 * 24 * time.Hour
	defaultArchiveRefreshInterval = 90 * 24 * time.Hour

	// refreshMinInterval is the floor for a non-zero interval. The scheduler
	// checks hourly, so anything shorter could not be honoured anyway, and a
	// misplaced "5m" would otherwise hammer TMDB every tick.
	refreshMinInterval = time.Hour

	// defaultJellyfinScanInterval is how often the Jellyfin library scan
	// (D-033) runs when a server is configured. A media library changes a few
	// times a day at most, and the settings page has a "scan now" for the
	// moment right after a download finishes.
	defaultJellyfinScanInterval = 6 * time.Hour
)

// Config is the complete, validated runtime configuration.
type Config struct {
	// BaseURL is the external URL the app is reachable at. It is the only
	// source of truth for absolute links, OIDC redirects, the PWA manifest
	// and the cookie Secure flag — never derived from request headers.
	BaseURL *url.URL

	// CookieSecure follows the scheme of BaseURL, not the local listener.
	CookieSecure bool

	// DataDir holds the SQLite file plus the assets/ and cache/ trees.
	DataDir string

	// DatabaseURL selects Postgres when set; empty means SQLite in DataDir.
	DatabaseURL string

	// ListenAddr is the plain-HTTP listen address; TLS is the proxy's job.
	ListenAddr string

	// TrustedProxies lists the peers whose X-Forwarded-* headers are
	// honoured. Empty means those headers are ignored entirely.
	TrustedProxies []netip.Prefix

	// TMDBAPIKeyV3 is TMDB's "API Key" — v3 auth, sent as an api_key query
	// parameter. TMDBAPIKeyV4 is its "API Read Access Token" — v4 auth, sent as
	// a Bearer header, and valid for the v3 endpoints this app uses.
	//
	// Either one alone is sufficient; when both are set the v4 token is used.
	// Both absent means adding items fails, and browsing does not. Shape is not
	// validated here — server/provider owns what a credential is.
	TMDBAPIKeyV3 string
	TMDBAPIKeyV4 string

	SteamAPIKey string

	// TMDBWatchRegion selects which country's "where to watch" listing the
	// video detail pages project out of a stored snapshot (D-022). ISO-3166-1
	// alpha-2, uppercase; "RO" when unset. It is never sent to TMDB — the
	// appended section carries every country, so changing this takes effect on
	// the next render with no refetch.
	TMDBWatchRegion string

	// DefaultTimezone is the IANA zone (D-038) of every user who has not chosen
	// one: it decides where their "today" ends — what has aired, what the
	// calendar opens on — and the zone the SPA renders instants in. "UTC" when
	// unset. Not read from TZ: Go feeds that into time.Local, which nothing in
	// this module may consult.
	DefaultTimezone string

	// TMDBRefreshInterval is how old a snapshot may get before the scheduled
	// ACTIVE refresh job re-fetches it: watchlisted movies and shows, and
	// followed shows still in production (D-025). Zero disables the job.
	TMDBRefreshInterval time.Duration

	// TMDBArchiveRefreshInterval is the same for the ARCHIVE job: watched
	// movies and followed shows TMDB marks Ended or Canceled, neither on any
	// watchlist. Zero disables the job.
	TMDBArchiveRefreshInterval time.Duration

	// OIDC configures OpenID Connect single sign-on. When it is enabled the
	// built-in username/password authentication is disabled entirely (D-018)
	// — the IdP becomes the only way in.
	OIDC OIDCConfig

	// Jellyfin names the household's Jellyfin server (D-033). When it is
	// enabled the video feature scans its library and the `jellyfin` copy
	// kind stops being a hand-set label: it mirrors what the server holds.
	Jellyfin JellyfinConfig

	// JellyfinScanInterval is how often that scan runs. Zero disables the
	// scheduled scan (the settings page's "scan now" still works); ignored
	// entirely when Jellyfin is not configured.
	JellyfinScanInterval time.Duration
}

// JellyfinConfig is the Jellyfin server connection. Enabled is true only when
// both the URL and the API key are present; one without the other is a
// startup error rather than a silent no-op, because a half-set pair would
// otherwise look exactly like "not configured" and never scan.
type JellyfinConfig struct {
	Enabled bool
	// URL is the server's base URL with no trailing slash — what a browser
	// reaches the web UI at, e.g. http://jellyfin.lan:8096.
	URL string
	// APIKey is an API key created in Jellyfin's dashboard. Server-side only:
	// never in a response, never logged.
	APIKey string
}

// OIDCConfig is the OpenID Connect configuration. Enabled is true only when
// issuer, client id and client secret are all present; a partial combination
// is a startup error rather than a silent fallback to local auth.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string

	// AdminClaim/AdminValue map an IdP group or role onto sofalog's admin
	// flag, re-evaluated on every login so the IdP stays authoritative. Both
	// empty means nobody is granted admin.
	AdminClaim string
	AdminValue string

	// Scopes requested at the authorize endpoint. "openid" is always present.
	Scopes []string

	// ProviderName labels the sign-in button in the UI.
	ProviderName string
}

// SQLitePath returns the SQLite database file path used when DatabaseURL is empty.
func (c Config) SQLitePath() string {
	return c.DataDir + "/sofalog.db"
}

// BasePath is the URL path the SPA is served under, always with a leading and
// a trailing slash ("/" when BASE_URL has no sub-path). It is what the served
// index.html declares as its <base href>, so the bundle's relative asset and
// /api/v1 URLs resolve against the app root rather than the current route.
func (c Config) BasePath() string {
	p := "/"
	if c.BaseURL != nil && c.BaseURL.Path != "" {
		p = c.BaseURL.Path
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// OIDCRedirectURI is the callback URL registered with the IdP, derived from
// BASE_URL and nothing else — never from Host or X-Forwarded-Host, which a
// client can spoof. Logged once at startup so the operator can paste it into
// the provider.
func (c Config) OIDCRedirectURI() string {
	if c.BaseURL == nil {
		return ""
	}
	return c.BaseURL.JoinPath("api/v1/auth/oidc/callback").String()
}

// TMDBLinkRedirectURI is where TMDB sends the browser back after a user
// approves linking their account (D-029). Derived from BASE_URL alone, like
// OIDCRedirectURI, for the same reason.
func (c Config) TMDBLinkRedirectURI() string {
	if c.BaseURL == nil {
		return ""
	}
	return c.BaseURL.JoinPath("api/v1/video/tmdb/link/callback").String()
}

// Load reads the process environment, layered over an optional .env file. It
// fails with one error naming every problem found, so a broken deployment
// surfaces everything at once — a malformed .env is reported alongside a missing
// BASE_URL rather than hiding it.
func Load() (Config, error) {
	fileVals, fileErr := readEnvFile(os.Getenv("ENV_FILE"))
	cfg, loadErr := load(envFileGetenv(fileVals))
	if err := errors.Join(fileErr, loadErr); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// readEnvFile locates and parses the optional .env file. Precedence: an explicit
// $ENV_FILE path, then .env in the working directory, then ../.env — the last one
// is what lets "cd server && go run ." pick up the repo-root file that docker
// compose interpolates from, so there is one .env per checkout, not two.
//
// A missing file is not an error; an $ENV_FILE naming a file that is not there is,
// because it can only be a deployment mistake. godotenv.Read is used rather than
// godotenv.Load: it returns the values instead of mutating the process
// environment, which keeps precedence explicit and tests free of os.Setenv.
func readEnvFile(explicit string) (map[string]string, error) {
	if explicit != "" {
		vals, err := godotenv.Read(explicit)
		if err != nil {
			return nil, fmt.Errorf("ENV_FILE %q: %v", explicit, err)
		}
		return vals, nil
	}
	for _, path := range []string{".env", "../.env"} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		vals, err := godotenv.Read(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		return vals, nil
	}
	return nil, nil
}

// envFileGetenv layers a .env file under the real process environment: a real
// variable always wins, so compose or systemd overrides a stale local file.
func envFileGetenv(file map[string]string) func(string) string {
	return func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return file[key]
	}
}

func load(getenv func(string) string) (Config, error) {
	var errs []error

	cfg := Config{
		DataDir:      "./data",
		ListenAddr:   ":8080",
		DatabaseURL:  getenv("DATABASE_URL"),
		TMDBAPIKeyV3: getenv("TMDB_API_KEY_V3"),
		TMDBAPIKeyV4: getenv("TMDB_API_KEY_V4"),
		SteamAPIKey:  getenv("STEAM_API_KEY"),

		TMDBWatchRegion:            "RO",
		DefaultTimezone:            "UTC",
		TMDBRefreshInterval:        defaultRefreshInterval,
		TMDBArchiveRefreshInterval: defaultArchiveRefreshInterval,
	}

	if v := getenv("DATA_DIR"); v != "" {
		cfg.DataDir = strings.TrimRight(v, "/")
	}
	if v := getenv("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	switch raw := getenv("BASE_URL"); raw {
	case "":
		errs = append(errs, errors.New("BASE_URL is required (e.g. https://media.example.com)"))
	default:
		u, err := url.Parse(strings.TrimRight(raw, "/"))
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("BASE_URL: %v", err))
		case u.Scheme != "http" && u.Scheme != "https":
			errs = append(errs, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", raw))
		case u.Host == "":
			errs = append(errs, fmt.Errorf("BASE_URL has no host: %q", raw))
		default:
			cfg.BaseURL = u
			cfg.CookieSecure = u.Scheme == "https"
		}
	}

	if raw := getenv("TRUSTED_PROXIES"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			p, err := parsePrefixOrAddr(part)
			if err != nil {
				errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %q is not a CIDR or IP", part))
				continue
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}

	// A wrong region is silent otherwise — the page would just show no
	// providers — so a malformed value is a startup error, not a fallback.
	if raw := strings.TrimSpace(getenv("TMDB_WATCH_REGION")); raw != "" {
		region := strings.ToUpper(raw)
		if !watchRegionRe.MatchString(region) {
			errs = append(errs, fmt.Errorf("TMDB_WATCH_REGION must be a two-letter ISO-3166-1 country code (e.g. US, DE, RO), got %q", raw))
		} else {
			cfg.TMDBWatchRegion = region
		}
	}

	// A misspelt zone would otherwise move every user's day silently, so it
	// is a startup error rather than a fallback to UTC.
	if raw := strings.TrimSpace(getenv("DEFAULT_TIMEZONE")); raw != "" {
		if _, err := tz.Load(raw); err != nil {
			errs = append(errs, fmt.Errorf("DEFAULT_TIMEZONE must be an IANA time zone name (e.g. UTC, Europe/Bucharest), got %q", raw))
		} else {
			cfg.DefaultTimezone = raw
		}
	}

	// Each job is validated on its own, so a typo in one still reports the
	// other's problem in the same startup error.
	if d, err := parseRefreshInterval("TMDB_REFRESH_INTERVAL", getenv("TMDB_REFRESH_INTERVAL"), defaultRefreshInterval); err != nil {
		errs = append(errs, err)
	} else {
		cfg.TMDBRefreshInterval = d
	}
	if d, err := parseRefreshInterval("TMDB_ARCHIVE_REFRESH_INTERVAL", getenv("TMDB_ARCHIVE_REFRESH_INTERVAL"), defaultArchiveRefreshInterval); err != nil {
		errs = append(errs, err)
	} else {
		cfg.TMDBArchiveRefreshInterval = d
	}

	if oidcErrs := loadOIDC(getenv, &cfg); len(oidcErrs) > 0 {
		errs = append(errs, oidcErrs...)
	}

	if jfErrs := loadJellyfin(getenv, &cfg); len(jfErrs) > 0 {
		errs = append(errs, jfErrs...)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// loadOIDC parses the OIDC block. All three of issuer, client id and secret
// are required together: a half-configured provider must never silently leave
// local authentication enabled, because that is precisely the surface single
// sign-on is meant to close.
func loadOIDC(getenv func(string) string, cfg *Config) []error {
	var errs []error
	o := OIDCConfig{
		Issuer:       strings.TrimRight(strings.TrimSpace(getenv("OIDC_ISSUER")), "/"),
		ClientID:     strings.TrimSpace(getenv("OIDC_CLIENT_ID")),
		ClientSecret: getenv("OIDC_CLIENT_SECRET"),
		AdminClaim:   strings.TrimSpace(getenv("OIDC_ADMIN_CLAIM")),
		AdminValue:   strings.TrimSpace(getenv("OIDC_ADMIN_VALUE")),
		ProviderName: strings.TrimSpace(getenv("OIDC_PROVIDER_NAME")),
	}

	set := map[string]bool{
		"OIDC_ISSUER":        o.Issuer != "",
		"OIDC_CLIENT_ID":     o.ClientID != "",
		"OIDC_CLIENT_SECRET": o.ClientSecret != "",
	}
	var missing []string
	for _, k := range []string{"OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET"} {
		if !set[k] {
			missing = append(missing, k)
		}
	}
	switch len(missing) {
	case 3:
		// Nothing configured: local auth stays on. Not an error.
		cfg.OIDC = OIDCConfig{}
		return nil
	case 0:
		o.Enabled = true
	default:
		return []error{fmt.Errorf("OIDC is partially configured: %s must also be set (set all three, or none)",
			strings.Join(missing, ", "))}
	}

	// Structural validation only — what a client secret *is* is the IdP's
	// business, matching how the TMDB credentials are treated.
	if u, err := url.Parse(o.Issuer); err != nil {
		errs = append(errs, fmt.Errorf("OIDC_ISSUER: %v", err))
	} else if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("OIDC_ISSUER must be an absolute http(s) URL, got %q", o.Issuer))
	}

	if (o.AdminClaim == "") != (o.AdminValue == "") {
		errs = append(errs, errors.New("OIDC_ADMIN_CLAIM and OIDC_ADMIN_VALUE must be set together"))
	}

	o.Scopes = []string{"openid", "profile", "email"}
	if raw := strings.TrimSpace(getenv("OIDC_SCOPES")); raw != "" {
		o.Scopes = nil
		for _, f := range strings.Fields(strings.ReplaceAll(raw, ",", " ")) {
			o.Scopes = append(o.Scopes, f)
		}
		if !slices.Contains(o.Scopes, "openid") {
			// Without it the provider is not doing OIDC at all and returns no
			// ID token, so add it rather than failing on a likely typo.
			o.Scopes = append([]string{"openid"}, o.Scopes...)
		}
	}

	if o.ProviderName == "" {
		o.ProviderName = "single sign-on"
	}

	cfg.OIDC = o
	return errs
}

// loadJellyfin parses the Jellyfin block (D-033): URL and API key together or
// not at all, plus the scan interval, which reuses the refresh jobs' parser
// because it is the same kind of value — "how often may a background job go
// out", with the same floor and the same 0-disables rule.
func loadJellyfin(getenv func(string) string, cfg *Config) []error {
	j := JellyfinConfig{
		URL:    strings.TrimRight(strings.TrimSpace(getenv("JELLYFIN_URL")), "/"),
		APIKey: strings.TrimSpace(getenv("JELLYFIN_API_KEY")),
	}
	var errs []error
	switch {
	case j.URL == "" && j.APIKey == "":
		// Not configured: the jellyfin copy stays a hand-set label. Not an
		// error, and the interval is irrelevant.
		cfg.Jellyfin = JellyfinConfig{}
		return nil
	case j.URL == "":
		return []error{errors.New("Jellyfin is partially configured: JELLYFIN_URL must also be set (set both, or neither)")}
	case j.APIKey == "":
		return []error{errors.New("Jellyfin is partially configured: JELLYFIN_API_KEY must also be set (set both, or neither)")}
	}
	if u, err := url.Parse(j.URL); err != nil {
		errs = append(errs, fmt.Errorf("JELLYFIN_URL: %v", err))
	} else if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("JELLYFIN_URL must be an absolute http(s) URL, got %q", j.URL))
	}
	j.Enabled = true
	cfg.Jellyfin = j

	if d, err := parseRefreshInterval("JELLYFIN_SCAN_INTERVAL", getenv("JELLYFIN_SCAN_INTERVAL"), defaultJellyfinScanInterval); err != nil {
		errs = append(errs, err)
	} else {
		cfg.JellyfinScanInterval = d
	}
	return errs
}

// parseRefreshInterval reads one refresh-job interval. Unset means the default;
// "0" disables the job. Otherwise the value is a whole number of days ("7d") —
// the unit an operator actually thinks in, which Go's duration syntax lacks —
// or a Go duration ("36h"). Mixed forms ("7d12h") are rejected rather than
// guessed at, and a non-zero value under refreshMinInterval is an error, because
// silently rounding it up would hide the mistake.
func parseRefreshInterval(name, raw string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	if raw == "0" {
		return 0, nil
	}
	malformed := fmt.Errorf("%s must be a number of days like 7d, a duration like 36h, or 0 to disable, got %q", name, raw)

	var d time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, malformed
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(raw); err != nil {
			return 0, malformed
		}
	}
	if d == 0 {
		return 0, nil
	}
	if d < refreshMinInterval {
		return 0, fmt.Errorf("%s must be at least %s, or 0 to disable, got %q", name, refreshMinInterval, raw)
	}
	return d, nil
}

// parsePrefixOrAddr accepts either a CIDR ("10.0.0.0/8") or a bare address
// ("172.18.0.2"), treating the latter as a single-host prefix.
func parsePrefixOrAddr(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
