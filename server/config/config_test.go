package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // substring; empty means success
		check   func(t *testing.T, c Config)
	}{
		{
			name:    "missing BASE_URL fails",
			env:     map[string]string{},
			wantErr: "BASE_URL is required",
		},
		{
			name:    "BASE_URL without scheme fails",
			env:     map[string]string{"BASE_URL": "media.example.com"},
			wantErr: "absolute http(s) URL",
		},
		{
			name:    "BASE_URL with bad scheme fails",
			env:     map[string]string{"BASE_URL": "ftp://media.example.com"},
			wantErr: "absolute http(s) URL",
		},
		{
			name: "defaults applied",
			env:  map[string]string{"BASE_URL": "https://media.example.com"},
			check: func(t *testing.T, c Config) {
				if c.DataDir != "./data" {
					t.Errorf("DataDir = %q, want ./data", c.DataDir)
				}
				if c.ListenAddr != ":8080" {
					t.Errorf("ListenAddr = %q, want :8080", c.ListenAddr)
				}
				if c.SQLitePath() != "./data/sofalog.db" {
					t.Errorf("SQLitePath() = %q", c.SQLitePath())
				}
				if len(c.TrustedProxies) != 0 {
					t.Errorf("TrustedProxies = %v, want empty", c.TrustedProxies)
				}
			},
		},
		{
			name: "cookie secure follows https scheme",
			env:  map[string]string{"BASE_URL": "https://media.example.com"},
			check: func(t *testing.T, c Config) {
				if !c.CookieSecure {
					t.Error("CookieSecure = false, want true for https")
				}
			},
		},
		{
			name: "cookie insecure for http",
			env:  map[string]string{"BASE_URL": "http://localhost:8080"},
			check: func(t *testing.T, c Config) {
				if c.CookieSecure {
					t.Error("CookieSecure = true, want false for http")
				}
			},
		},
		{
			name: "trailing slash stripped from BASE_URL",
			env:  map[string]string{"BASE_URL": "https://media.example.com/"},
			check: func(t *testing.T, c Config) {
				if c.BaseURL.String() != "https://media.example.com" {
					t.Errorf("BaseURL = %q", c.BaseURL)
				}
			},
		},
		{
			name: "trusted proxies parse CIDRs and bare IPs",
			env: map[string]string{
				"BASE_URL":        "https://media.example.com",
				"TRUSTED_PROXIES": "10.0.0.0/8, 172.18.0.2",
			},
			check: func(t *testing.T, c Config) {
				if len(c.TrustedProxies) != 2 {
					t.Fatalf("got %d prefixes, want 2", len(c.TrustedProxies))
				}
				if got := c.TrustedProxies[1].String(); got != "172.18.0.2/32" {
					t.Errorf("bare IP parsed as %q, want 172.18.0.2/32", got)
				}
			},
		},
		{
			name: "bad trusted proxy fails",
			env: map[string]string{
				"BASE_URL":        "https://media.example.com",
				"TRUSTED_PROXIES": "not-an-ip",
			},
			wantErr: "not a CIDR or IP",
		},
		{
			name:    "all problems reported at once",
			env:     map[string]string{"TRUSTED_PROXIES": "junk"},
			wantErr: "BASE_URL is required",
		},
		{
			name: "overrides respected",
			env: map[string]string{
				"BASE_URL":    "https://media.example.com",
				"DATA_DIR":    "/var/lib/sofalog/",
				"LISTEN_ADDR": ":9090",
			},
			check: func(t *testing.T, c Config) {
				if c.DataDir != "/var/lib/sofalog" {
					t.Errorf("DataDir = %q, want trailing slash stripped", c.DataDir)
				}
				if c.ListenAddr != ":9090" {
					t.Errorf("ListenAddr = %q", c.ListenAddr)
				}
			},
		},
		{
			name: "provider credentials are optional and read verbatim",
			env: map[string]string{
				"BASE_URL":        "https://media.example.com",
				"TMDB_API_KEY_V3": "  abc123  ",
				"TMDB_API_KEY_V4": "eyJhbGciOiJIUzI1NiJ9.payload.sig",
				"STEAM_API_KEY":   "steamkey",
			},
			check: func(t *testing.T, c Config) {
				// config neither trims nor validates shape: server/provider owns
				// what a credential is, so there is exactly one normaliser.
				if c.TMDBAPIKeyV3 != "  abc123  " {
					t.Errorf("TMDBAPIKeyV3 = %q", c.TMDBAPIKeyV3)
				}
				if c.TMDBAPIKeyV4 != "eyJhbGciOiJIUzI1NiJ9.payload.sig" {
					t.Errorf("TMDBAPIKeyV4 = %q", c.TMDBAPIKeyV4)
				}
				if c.SteamAPIKey != "steamkey" {
					t.Errorf("SteamAPIKey = %q", c.SteamAPIKey)
				}
			},
		},
		{
			name: "the watch region is trimmed and uppercased",
			env: map[string]string{
				"BASE_URL":          "https://media.example.com",
				"TMDB_WATCH_REGION": " de ",
			},
			check: func(t *testing.T, c Config) {
				if c.TMDBWatchRegion != "DE" {
					t.Errorf("TMDBWatchRegion = %q, want DE", c.TMDBWatchRegion)
				}
			},
		},
		{
			name: "an unset watch region falls back to RO",
			env:  map[string]string{"BASE_URL": "https://media.example.com"},
			check: func(t *testing.T, c Config) {
				if c.TMDBWatchRegion != "RO" {
					t.Errorf("TMDBWatchRegion = %q, want the RO default", c.TMDBWatchRegion)
				}
			},
		},
		{
			name: "the default timezone is UTC when unset",
			env:  map[string]string{"BASE_URL": "https://media.example.com"},
			check: func(t *testing.T, c Config) {
				if c.DefaultTimezone != "UTC" {
					t.Errorf("DefaultTimezone = %q, want UTC", c.DefaultTimezone)
				}
			},
		},
		{
			name: "a valid default timezone is kept as named",
			env: map[string]string{
				"BASE_URL":         "https://media.example.com",
				"DEFAULT_TIMEZONE": " Europe/Bucharest ",
			},
			check: func(t *testing.T, c Config) {
				if c.DefaultTimezone != "Europe/Bucharest" {
					t.Errorf("DefaultTimezone = %q, want Europe/Bucharest", c.DefaultTimezone)
				}
			},
		},
		{
			name:    "an unknown default timezone is a startup error",
			env:     map[string]string{"BASE_URL": "https://media.example.com", "DEFAULT_TIMEZONE": "Europe/Atlantis"},
			wantErr: "DEFAULT_TIMEZONE",
		},
		{
			name:    "Local is refused as a default timezone",
			env:     map[string]string{"BASE_URL": "https://media.example.com", "DEFAULT_TIMEZONE": "Local"},
			wantErr: "DEFAULT_TIMEZONE",
		},
		{
			name: "either TMDB credential alone is read independently",
			env: map[string]string{
				"BASE_URL":        "https://media.example.com",
				"TMDB_API_KEY_V4": "eyJonly",
			},
			check: func(t *testing.T, c Config) {
				if c.TMDBAPIKeyV3 != "" {
					t.Errorf("TMDBAPIKeyV3 = %q, want empty", c.TMDBAPIKeyV3)
				}
				if c.TMDBAPIKeyV4 != "eyJonly" {
					t.Errorf("TMDBAPIKeyV4 = %q", c.TMDBAPIKeyV4)
				}
			},
		},
		{
			name: "absent provider credentials are not an error",
			env:  map[string]string{"BASE_URL": "https://media.example.com"},
			check: func(t *testing.T, c Config) {
				if c.TMDBAPIKeyV3 != "" || c.TMDBAPIKeyV4 != "" {
					t.Errorf("TMDB credentials = %q / %q, want both empty", c.TMDBAPIKeyV3, c.TMDBAPIKeyV4)
				}
				if c.SteamAPIKey != "" {
					t.Errorf("SteamAPIKey = %q, want empty", c.SteamAPIKey)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(envMap(tt.env))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}

	// A wrong region would otherwise be silent — the pages would just list no
	// providers — so it is a startup error, like every other malformed value.
	t.Run("a malformed watch region is a startup error", func(t *testing.T) {
		t.Parallel()
		_, err := load(envMap(map[string]string{
			"BASE_URL":          "https://media.example.com",
			"TMDB_WATCH_REGION": "USA",
		}))
		if err == nil || !strings.Contains(err.Error(), "TMDB_WATCH_REGION") {
			t.Fatalf("want a TMDB_WATCH_REGION error, got %v", err)
		}
	})

	t.Run("both BASE_URL and proxy errors joined", func(t *testing.T) {
		t.Parallel()
		_, err := load(envMap(map[string]string{"TRUSTED_PROXIES": "junk"}))
		if err == nil || !strings.Contains(err.Error(), "not a CIDR or IP") {
			t.Fatalf("want joined error to include proxy problem, got %v", err)
		}
	})
}

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing temp env file: %v", err)
	}
	return path
}

// TestReadEnvFileExplicit covers the $ENV_FILE path, which needs no chdir and so
// can run in parallel.
func TestReadEnvFileExplicit(t *testing.T) {
	t.Parallel()

	t.Run("explicit path is parsed", func(t *testing.T) {
		t.Parallel()
		path := writeEnvFile(t, "BASE_URL=http://localhost:8080\nTMDB_API_KEY_V3=fromfile\n")
		vals, err := readEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vals["TMDB_API_KEY_V3"] != "fromfile" {
			t.Errorf("TMDB_API_KEY_V3 = %q, want fromfile", vals["TMDB_API_KEY_V3"])
		}
	})

	t.Run("explicit path that does not exist is an error", func(t *testing.T) {
		t.Parallel()
		_, err := readEnvFile(filepath.Join(t.TempDir(), "absent"))
		if err == nil || !strings.Contains(err.Error(), "ENV_FILE") {
			t.Fatalf("want an ENV_FILE error, got %v", err)
		}
	})

}

// TestReadEnvFileDiscovery covers the working-directory search. Every subtest
// uses t.Chdir, which the runtime forbids combining with t.Parallel — anywhere in
// the parent chain — so neither this test nor its subtests are parallel.
func TestReadEnvFileDiscovery(t *testing.T) {
	t.Run("no file anywhere is not an error", func(t *testing.T) {
		// Not parallel: t.Chdir cannot be combined with t.Parallel.
		t.Chdir(t.TempDir())
		vals, err := readEnvFile("")
		if err != nil {
			t.Fatalf("a missing .env must not be an error, got %v", err)
		}
		if len(vals) != 0 {
			t.Errorf("vals = %v, want empty", vals)
		}
	})

	t.Run("discovers .env in the working directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TMDB_API_KEY_V3=cwd\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		vals, err := readEnvFile("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vals["TMDB_API_KEY_V3"] != "cwd" {
			t.Errorf("TMDB_API_KEY_V3 = %q, want cwd", vals["TMDB_API_KEY_V3"])
		}
	})

	t.Run("falls back to ../.env so cd server && go run . works", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TMDB_API_KEY_V3=parent\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(root, "server")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(sub)
		vals, err := readEnvFile("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vals["TMDB_API_KEY_V3"] != "parent" {
			t.Errorf("TMDB_API_KEY_V3 = %q, want parent", vals["TMDB_API_KEY_V3"])
		}
	})

}

func TestReadEnvFileMalformed(t *testing.T) {
	t.Parallel()

	// A .env with a line that is not KEY=VALUE must be reported rather than
	// silently half-parsed: a config file that looks applied but is not is the
	// worst outcome for something holding an API key.
	path := writeEnvFile(t, "TMDB_API_KEY_V3=ok\nthis line has no equals sign\n")
	if _, err := readEnvFile(path); err == nil {
		t.Fatal("want an error for a malformed env file, got nil")
	}
}

func TestEnvFileGetenv(t *testing.T) {
	// Not parallel: this test sets a real process environment variable, which is
	// the only way to assert the precedence rule that matters in production.
	file := map[string]string{"TMDB_API_KEY_V3": "fromfile", "BASE_URL": "http://from.file"}
	getenv := envFileGetenv(file)

	if got := getenv("TMDB_API_KEY_V3"); got != "fromfile" {
		t.Errorf("with no process env set, got %q, want fromfile", got)
	}

	t.Setenv("TMDB_API_KEY_V3", "fromenv")
	if got := getenv("TMDB_API_KEY_V3"); got != "fromenv" {
		t.Errorf("the process environment must win over .env: got %q, want fromenv", got)
	}
	if got := getenv("BASE_URL"); got != "http://from.file" {
		t.Errorf("unset variables must still fall through to the file: got %q", got)
	}
	if got := getenv("NOT_SET_ANYWHERE"); got != "" {
		t.Errorf("unknown key = %q, want empty", got)
	}
}

func TestLoadOIDC(t *testing.T) {
	t.Parallel()

	base := func(extra map[string]string) map[string]string {
		env := map[string]string{"BASE_URL": "https://media.example.com"}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	full := map[string]string{
		"OIDC_ISSUER":        "https://idp.example.com/realms/home",
		"OIDC_CLIENT_ID":     "sofalog",
		"OIDC_CLIENT_SECRET": "s3cret",
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(t *testing.T, c Config)
	}{
		{
			name: "absent OIDC is not an error and leaves local auth on",
			env:  base(nil),
			check: func(t *testing.T, c Config) {
				if c.OIDC.Enabled {
					t.Error("OIDC enabled without any configuration")
				}
			},
		},
		{
			name: "all three set enables it",
			env:  base(full),
			check: func(t *testing.T, c Config) {
				if !c.OIDC.Enabled {
					t.Fatal("OIDC not enabled with a full configuration")
				}
				if c.OIDC.ProviderName != "single sign-on" {
					t.Errorf("provider name = %q, want the default", c.OIDC.ProviderName)
				}
				if got := strings.Join(c.OIDC.Scopes, " "); got != "openid profile email" {
					t.Errorf("scopes = %q, want the default", got)
				}
			},
		},
		// A partial block must fail loudly: silently falling back would leave
		// password login open on an instance meant to be SSO-only.
		{
			name:    "issuer alone fails",
			env:     base(map[string]string{"OIDC_ISSUER": "https://idp.example.com"}),
			wantErr: "OIDC_CLIENT_ID, OIDC_CLIENT_SECRET must also be set",
		},
		{
			name: "missing secret fails",
			env: base(map[string]string{
				"OIDC_ISSUER":    "https://idp.example.com",
				"OIDC_CLIENT_ID": "sofalog",
			}),
			wantErr: "OIDC_CLIENT_SECRET must also be set",
		},
		{
			name: "issuer without a scheme fails",
			env: base(map[string]string{
				"OIDC_ISSUER":        "idp.example.com",
				"OIDC_CLIENT_ID":     "sofalog",
				"OIDC_CLIENT_SECRET": "s3cret",
			}),
			wantErr: "absolute http(s) URL",
		},
		{
			name: "admin claim without a value fails",
			env: base(map[string]string{
				"OIDC_ISSUER": "https://idp.example.com", "OIDC_CLIENT_ID": "sofalog",
				"OIDC_CLIENT_SECRET": "s3cret", "OIDC_ADMIN_CLAIM": "groups",
			}),
			wantErr: "must be set together",
		},
		{
			name: "admin claim and value together",
			env: base(map[string]string{
				"OIDC_ISSUER": "https://idp.example.com", "OIDC_CLIENT_ID": "sofalog",
				"OIDC_CLIENT_SECRET": "s3cret", "OIDC_ADMIN_CLAIM": "groups",
				"OIDC_ADMIN_VALUE": "sofalog-admins",
			}),
			check: func(t *testing.T, c Config) {
				if c.OIDC.AdminClaim != "groups" || c.OIDC.AdminValue != "sofalog-admins" {
					t.Errorf("admin mapping = %q/%q", c.OIDC.AdminClaim, c.OIDC.AdminValue)
				}
			},
		},
		{
			name: "custom scopes always keep openid",
			env: base(map[string]string{
				"OIDC_ISSUER": "https://idp.example.com", "OIDC_CLIENT_ID": "sofalog",
				"OIDC_CLIENT_SECRET": "s3cret", "OIDC_SCOPES": "profile,email,groups",
			}),
			check: func(t *testing.T, c Config) {
				if got := strings.Join(c.OIDC.Scopes, " "); got != "openid profile email groups" {
					t.Errorf("scopes = %q, want openid prepended", got)
				}
			},
		},
		{
			name: "trailing slash on the issuer is trimmed",
			env: base(map[string]string{
				"OIDC_ISSUER": "https://idp.example.com/realms/home/", "OIDC_CLIENT_ID": "sofalog",
				"OIDC_CLIENT_SECRET": "s3cret",
			}),
			check: func(t *testing.T, c Config) {
				// go-oidc compares the discovery document's issuer to this
				// string exactly, so a stray slash breaks verification.
				if c.OIDC.Issuer != "https://idp.example.com/realms/home" {
					t.Errorf("issuer = %q", c.OIDC.Issuer)
				}
			},
		},
		{
			name: "refresh intervals default to 7d and 90d",
			env:  base(nil),
			check: func(t *testing.T, c Config) {
				if c.TMDBRefreshInterval != 7*24*time.Hour {
					t.Errorf("TMDBRefreshInterval = %s, want 168h", c.TMDBRefreshInterval)
				}
				if c.TMDBArchiveRefreshInterval != 90*24*time.Hour {
					t.Errorf("TMDBArchiveRefreshInterval = %s, want 2160h", c.TMDBArchiveRefreshInterval)
				}
			},
		},
		{
			name: "refresh intervals accept days and Go durations",
			env:  base(map[string]string{"TMDB_REFRESH_INTERVAL": "3d", "TMDB_ARCHIVE_REFRESH_INTERVAL": " 36h "}),
			check: func(t *testing.T, c Config) {
				if c.TMDBRefreshInterval != 72*time.Hour {
					t.Errorf("TMDBRefreshInterval = %s, want 72h", c.TMDBRefreshInterval)
				}
				if c.TMDBArchiveRefreshInterval != 36*time.Hour {
					t.Errorf("TMDBArchiveRefreshInterval = %s, want 36h", c.TMDBArchiveRefreshInterval)
				}
			},
		},
		{
			name: "zero disables a refresh job independently",
			env:  base(map[string]string{"TMDB_ARCHIVE_REFRESH_INTERVAL": "0"}),
			check: func(t *testing.T, c Config) {
				if c.TMDBRefreshInterval != 7*24*time.Hour || c.TMDBArchiveRefreshInterval != 0 {
					t.Errorf("intervals = %s / %s, want 168h / 0",
						c.TMDBRefreshInterval, c.TMDBArchiveRefreshInterval)
				}
			},
		},
		{
			name:    "refresh interval under an hour fails",
			env:     base(map[string]string{"TMDB_REFRESH_INTERVAL": "30m"}),
			wantErr: "TMDB_REFRESH_INTERVAL must be at least 1h0m0s",
		},
		{
			name:    "mixed day and hour form fails",
			env:     base(map[string]string{"TMDB_REFRESH_INTERVAL": "7d12h"}),
			wantErr: `TMDB_REFRESH_INTERVAL must be a number of days like 7d, a duration like 36h, or 0 to disable, got "7d12h"`,
		},
		{
			name:    "negative refresh interval fails",
			env:     base(map[string]string{"TMDB_ARCHIVE_REFRESH_INTERVAL": "-1h"}),
			wantErr: "TMDB_ARCHIVE_REFRESH_INTERVAL must be at least",
		},
		{
			name:    "garbage refresh interval fails",
			env:     base(map[string]string{"TMDB_ARCHIVE_REFRESH_INTERVAL": "abc"}),
			wantErr: `TMDB_ARCHIVE_REFRESH_INTERVAL must be a number of days`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(envMap(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

func TestLoadJellyfin(t *testing.T) {
	t.Parallel()

	base := func(extra map[string]string) map[string]string {
		env := map[string]string{"BASE_URL": "https://media.example.com"}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		check   func(t *testing.T, c Config)
	}{
		{
			name: "absent Jellyfin is not an error",
			env:  base(nil),
			check: func(t *testing.T, c Config) {
				if c.Jellyfin.Enabled || c.JellyfinScanInterval != 0 {
					t.Errorf("Jellyfin = %+v, interval %s; want disabled", c.Jellyfin, c.JellyfinScanInterval)
				}
			},
		},
		{
			name: "url and key enable it with the default interval, slash trimmed",
			env:  base(map[string]string{"JELLYFIN_URL": "http://jf.lan:8096/", "JELLYFIN_API_KEY": "k"}),
			check: func(t *testing.T, c Config) {
				if !c.Jellyfin.Enabled || c.Jellyfin.URL != "http://jf.lan:8096" || c.Jellyfin.APIKey != "k" {
					t.Errorf("Jellyfin = %+v", c.Jellyfin)
				}
				if c.JellyfinScanInterval != defaultJellyfinScanInterval {
					t.Errorf("interval = %s, want the default", c.JellyfinScanInterval)
				}
			},
		},
		// A half-set pair would otherwise look exactly like "not configured"
		// and silently never scan.
		{
			name:    "url alone fails",
			env:     base(map[string]string{"JELLYFIN_URL": "http://jf.lan:8096"}),
			wantErr: "JELLYFIN_API_KEY must also be set",
		},
		{
			name:    "key alone fails",
			env:     base(map[string]string{"JELLYFIN_API_KEY": "k"}),
			wantErr: "JELLYFIN_URL must also be set",
		},
		{
			name:    "url without a scheme fails",
			env:     base(map[string]string{"JELLYFIN_URL": "jf.lan:8096", "JELLYFIN_API_KEY": "k"}),
			wantErr: "absolute http(s) URL",
		},
		{
			name: "interval parses days, durations and zero",
			env:  base(map[string]string{"JELLYFIN_URL": "http://jf.lan", "JELLYFIN_API_KEY": "k", "JELLYFIN_SCAN_INTERVAL": "1d"}),
			check: func(t *testing.T, c Config) {
				if c.JellyfinScanInterval != 24*time.Hour {
					t.Errorf("interval = %s, want 24h", c.JellyfinScanInterval)
				}
			},
		},
		{
			name: "zero interval disables the schedule but keeps the server",
			env:  base(map[string]string{"JELLYFIN_URL": "http://jf.lan", "JELLYFIN_API_KEY": "k", "JELLYFIN_SCAN_INTERVAL": "0"}),
			check: func(t *testing.T, c Config) {
				if !c.Jellyfin.Enabled || c.JellyfinScanInterval != 0 {
					t.Errorf("Jellyfin = %+v, interval %s", c.Jellyfin, c.JellyfinScanInterval)
				}
			},
		},
		{
			name:    "an interval under an hour fails",
			env:     base(map[string]string{"JELLYFIN_URL": "http://jf.lan", "JELLYFIN_API_KEY": "k", "JELLYFIN_SCAN_INTERVAL": "5m"}),
			wantErr: "JELLYFIN_SCAN_INTERVAL must be at least 1h0m0s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(envMap(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

// The redirect URI must come from BASE_URL alone, sub-path included.
func TestOIDCRedirectURI(t *testing.T) {
	t.Parallel()
	tests := []struct{ baseURL, want string }{
		{"https://media.example.com", "https://media.example.com/api/v1/auth/oidc/callback"},
		{"https://example.com/media", "https://example.com/media/api/v1/auth/oidc/callback"},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(envMap(map[string]string{"BASE_URL": tt.baseURL}))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := cfg.OIDCRedirectURI(); got != tt.want {
				t.Errorf("redirect uri = %q, want %q", got, tt.want)
			}
		})
	}
}

// The TMDB link callback (D-029) follows the same rule: BASE_URL alone.
func TestTMDBLinkRedirectURI(t *testing.T) {
	t.Parallel()
	tests := []struct{ baseURL, want string }{
		{"https://media.example.com", "https://media.example.com/api/v1/video/tmdb/link/callback"},
		{"https://example.com/media", "https://example.com/media/api/v1/video/tmdb/link/callback"},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(envMap(map[string]string{"BASE_URL": tt.baseURL}))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := cfg.TMDBLinkRedirectURI(); got != tt.want {
				t.Errorf("redirect uri = %q, want %q", got, tt.want)
			}
		})
	}
}
