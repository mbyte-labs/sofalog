package provider

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

// The user-session half of the client (D-029): the session id is a secret
// exactly like the key, the write endpoints take a JSON body with TMDB's
// charset content type, and a refused session must be distinguishable from a
// refused key — the worker acts on one and treats the other as an outage.

func TestTMDBSetRatingRequestShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		creds      TMDBCredentials
		wantKey    bool // api_key in the query
		wantBearer bool
	}{
		{name: "v3 key: api_key and session_id in the query", creds: TMDBCredentials{APIKeyV3: "v3key"}, wantKey: true},
		{name: "v4 token: bearer header, session_id in the query", creds: TMDBCredentials{APIKeyV4: "eyJv4"}, wantBearer: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubDoer{status: http.StatusCreated, body: `{"status_code":1,"status_message":"Success."}`}
			c := NewTMDB(tt.creds, WithHTTPDoer(stub))
			err := c.SetRating(t.Context(), "sess-1", RatingTarget{Kind: "movie", TMDBID: 603}, 7)
			if err != nil {
				t.Fatalf("SetRating: %v", err)
			}
			if len(stub.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(stub.requests))
			}
			req := stub.requests[0]
			if req.Method != http.MethodPost || req.URL.Path != "/3/movie/603/rating" {
				t.Errorf("request = %s %s", req.Method, req.URL.Path)
			}
			q := req.URL.Query()
			if q.Get("session_id") != "sess-1" {
				t.Errorf("session_id = %q, want sess-1", q.Get("session_id"))
			}
			if (q.Get("api_key") != "") != tt.wantKey {
				t.Errorf("api_key present = %v, want %v", q.Get("api_key") != "", tt.wantKey)
			}
			if (req.Header.Get("Authorization") != "") != tt.wantBearer {
				t.Errorf("Authorization present = %v, want %v", req.Header.Get("Authorization") != "", tt.wantBearer)
			}
			if ct := req.Header.Get("Content-Type"); ct != "application/json;charset=utf-8" {
				t.Errorf("Content-Type = %q", ct)
			}
			raw, _ := io.ReadAll(req.Body)
			if string(raw) != `{"value":7}` {
				t.Errorf("body = %s, want {\"value\":7}", raw)
			}
		})
	}
}

func TestTMDBRatingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  RatingTarget
		want    string
		wantErr bool
	}{
		{name: "movie", target: RatingTarget{Kind: "movie", TMDBID: 603}, want: "/movie/603/rating"},
		{name: "show", target: RatingTarget{Kind: "show", TMDBID: 1399}, want: "/tv/1399/rating"},
		{name: "episode is addressed by show and position", target: RatingTarget{Kind: "episode", TMDBID: 1399, Season: 2, Episode: 9}, want: "/tv/1399/season/2/episode/9/rating"},
		{name: "season has no route", target: RatingTarget{Kind: "season", TMDBID: 5}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ratingPath(tt.target)
			if tt.wantErr {
				if !errors.Is(err, httpx.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("ratingPath = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestTMDBSessionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stub stubDoer
		call func(c *TMDB) error
		want error // sentinel; nil means success
	}{
		{
			name: "201 on a first rating is success",
			stub: stubDoer{status: http.StatusCreated, body: `{"status_code":1}`},
			call: func(c *TMDB) error { return c.SetRating(t.Context(), "s", RatingTarget{Kind: "movie", TMDBID: 1}, 5) },
		},
		{
			name: "200 on a delete is success",
			stub: stubDoer{status: http.StatusOK, body: `{"status_code":13}`},
			call: func(c *TMDB) error { return c.DeleteRating(t.Context(), "s", RatingTarget{Kind: "show", TMDBID: 1}) },
		},
		{
			name: "401 with the invalid-key code is an outage, not a broken session",
			stub: stubDoer{status: http.StatusUnauthorized, body: `{"status_code":7,"status_message":"Invalid API key"}`},
			call: func(c *TMDB) error { return c.SetRating(t.Context(), "s", RatingTarget{Kind: "movie", TMDBID: 1}, 5) },
			want: httpx.ErrProviderUnavailable,
		},
		{
			name: "401 with a session on the request is a broken session",
			stub: stubDoer{status: http.StatusUnauthorized, body: `{"status_code":3,"status_message":"Authentication failed"}`},
			call: func(c *TMDB) error { return c.SetRating(t.Context(), "s", RatingTarget{Kind: "movie", TMDBID: 1}, 5) },
			want: ErrSessionInvalid,
		},
		{
			name: "401 exchanging a request token is a refused link",
			stub: stubDoer{status: http.StatusUnauthorized, body: `{"status_code":17}`},
			call: func(c *TMDB) error { _, err := c.CreateSession(t.Context(), "tok"); return err },
			want: ErrSessionInvalid,
		},
		{
			name: "401 on a plain read stays the key error",
			stub: stubDoer{status: http.StatusUnauthorized, body: `{"status_code":3}`},
			call: func(c *TMDB) error { _, err := c.NewRequestToken(t.Context()); return err },
			want: httpx.ErrProviderUnavailable,
		},
		{
			name: "404 is not found",
			stub: stubDoer{status: http.StatusNotFound, body: `{"status_code":34}`},
			call: func(c *TMDB) error { return c.SetRating(t.Context(), "s", RatingTarget{Kind: "movie", TMDBID: 1}, 5) },
			want: httpx.ErrNotFound,
		},
		{
			name: "400 is the request's fault",
			stub: stubDoer{status: http.StatusBadRequest, body: `{"status_code":18}`},
			call: func(c *TMDB) error { return c.SetRating(t.Context(), "s", RatingTarget{Kind: "movie", TMDBID: 1}, 5) },
			want: httpx.ErrValidation,
		},
		{
			name: "500 is an outage",
			stub: stubDoer{status: http.StatusInternalServerError, body: `oops`},
			call: func(c *TMDB) error { return c.DeleteSession(t.Context(), "s") },
			want: httpx.ErrProviderUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := tt.stub
			c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(&stub))
			err := tt.call(c)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if errors.Is(err, ErrSessionInvalid) && errors.Is(err, httpx.ErrProviderUnavailable) {
				t.Errorf("a broken session must not also read as an outage: %v", err)
			}
		})
	}
}

func TestTMDBSessionSecretsNeverReachErrors(t *testing.T) {
	t.Parallel()

	// A transport error embeds the full URL — key AND session — and TMDB
	// echoes bodies back. Neither value may survive into err.Error().
	stub := &stubDoer{err: errors.New(`Post "https://api.themoviedb.org/3/movie/1/rating?api_key=v3key&session_id=sess-secret": dial tcp: timeout`)}
	c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
	err := c.SetRating(t.Context(), "sess-secret", RatingTarget{Kind: "movie", TMDBID: 1}, 5)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"v3key", "sess-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks %q: %v", secret, err)
		}
	}

	got := redact(`body {"session_id":"abc","request_token":"tok"} url ?request_token=tok2&x=1`)
	for _, secret := range []string{"abc", "tok", "tok2"} {
		if strings.Contains(got, `"`+secret+`"`) || strings.Contains(got, "="+secret) {
			t.Errorf("redact left %q in %q", secret, got)
		}
	}
}

func TestTMDBLinkFlowCalls(t *testing.T) {
	t.Parallel()

	t.Run("request token", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"success":true,"request_token":"tok-1"}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
		tok, err := c.NewRequestToken(t.Context())
		if err != nil || tok != "tok-1" {
			t.Fatalf("NewRequestToken = %q, %v", tok, err)
		}
		if p := stub.requests[0].URL.Path; p != "/3/authentication/token/new" {
			t.Errorf("path = %s", p)
		}
	})

	t.Run("authenticate url escapes the redirect", func(t *testing.T) {
		t.Parallel()
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"})
		got := c.AuthenticateURL("tok/1", "https://sofalog.test/app/api/v1/video/tmdb/link/callback")
		want := "https://www.themoviedb.org/authenticate/tok%2F1?redirect_to=https%3A%2F%2Fsofalog.test%2Fapp%2Fapi%2Fv1%2Fvideo%2Ftmdb%2Flink%2Fcallback"
		if got != want {
			t.Errorf("AuthenticateURL = %q, want %q", got, want)
		}
	})

	t.Run("create session posts the token in the body", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"success":true,"session_id":"sess-1"}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
		sess, err := c.CreateSession(t.Context(), "tok-1")
		if err != nil || sess != "sess-1" {
			t.Fatalf("CreateSession = %q, %v", sess, err)
		}
		req := stub.requests[0]
		raw, _ := io.ReadAll(req.Body)
		if req.Method != http.MethodPost || string(raw) != `{"request_token":"tok-1"}` {
			t.Errorf("request = %s %s", req.Method, raw)
		}
	})

	t.Run("account reads id and username with the session", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"id":42,"username":"bob"}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
		acc, err := c.Account(t.Context(), "sess-1")
		if err != nil || acc.ID != 42 || acc.Username != "bob" {
			t.Fatalf("Account = %+v, %v", acc, err)
		}
		if q := stub.requests[0].URL.Query().Get("session_id"); q != "sess-1" {
			t.Errorf("session_id = %q", q)
		}
	})

	t.Run("set watchlist posts media_type, media_id and the flag with the session", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			kind, want string
			on         bool
		}{
			{"movie", `{"media_id":603,"media_type":"movie","watchlist":true}`, true},
			{"show", `{"media_id":1399,"media_type":"tv","watchlist":false}`, false},
		}
		for _, tt := range tests {
			stub := &stubDoer{status: http.StatusCreated, body: `{"status_code":1}`}
			c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
			id := int64(603)
			if tt.kind == "show" {
				id = 1399
			}
			if err := c.SetWatchlist(t.Context(), "sess-1", 42, tt.kind, id, tt.on); err != nil {
				t.Fatalf("SetWatchlist(%s): %v", tt.kind, err)
			}
			req := stub.requests[0]
			raw, _ := io.ReadAll(req.Body)
			if req.Method != http.MethodPost || req.URL.Path != "/3/account/42/watchlist" || string(raw) != tt.want {
				t.Errorf("request = %s %s %s, want body %s", req.Method, req.URL.Path, raw, tt.want)
			}
			if q := req.URL.Query().Get("session_id"); q != "sess-1" {
				t.Errorf("session_id = %q", q)
			}
		}
		if err := NewTMDB(TMDBCredentials{APIKeyV3: "k"}).SetWatchlist(t.Context(), "s", 1, "episode", 1, true); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("episode err = %v, want ErrValidation", err)
		}
	})

	// The favorite call is the watchlist's twin (D-030): same route family,
	// same body shape with the flag renamed.
	t.Run("set favorite posts to the favorite route with the favorite flag", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			kind, want string
			on         bool
		}{
			{"movie", `{"favorite":true,"media_id":603,"media_type":"movie"}`, true},
			{"show", `{"favorite":false,"media_id":1399,"media_type":"tv"}`, false},
		}
		for _, tt := range tests {
			stub := &stubDoer{status: http.StatusCreated, body: `{"status_code":1}`}
			c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
			id := int64(603)
			if tt.kind == "show" {
				id = 1399
			}
			if err := c.SetFavorite(t.Context(), "sess-1", 42, tt.kind, id, tt.on); err != nil {
				t.Fatalf("SetFavorite(%s): %v", tt.kind, err)
			}
			req := stub.requests[0]
			raw, _ := io.ReadAll(req.Body)
			if req.Method != http.MethodPost || req.URL.Path != "/3/account/42/favorite" || string(raw) != tt.want {
				t.Errorf("request = %s %s %s, want body %s", req.Method, req.URL.Path, raw, tt.want)
			}
			if q := req.URL.Query().Get("session_id"); q != "sess-1" {
				t.Errorf("session_id = %q", q)
			}
		}
		if err := NewTMDB(TMDBCredentials{APIKeyV3: "k"}).SetFavorite(t.Context(), "s", 1, "episode", 1, true); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("episode err = %v, want ErrValidation", err)
		}
	})

	t.Run("delete session sends the id in the body", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"success":true}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
		if err := c.DeleteSession(t.Context(), "sess-1"); err != nil {
			t.Fatalf("DeleteSession: %v", err)
		}
		req := stub.requests[0]
		raw, _ := io.ReadAll(req.Body)
		if req.Method != http.MethodDelete || req.URL.Path != "/3/authentication/session" || string(raw) != `{"session_id":"sess-1"}` {
			t.Errorf("request = %s %s %s", req.Method, req.URL.Path, raw)
		}
	})
}

// The account list readers (D-036): the one read of a user's lists, at setup.

func TestTMDBAccountListRequestShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		call     func(c *TMDB) error
		wantPath string
	}{
		{name: "rated movies", call: func(c *TMDB) error { _, err := c.AccountRatedMovies(t.Context(), "sess-1", 42, 3); return err }, wantPath: "/3/account/42/rated/movies"},
		{name: "rated shows", call: func(c *TMDB) error { _, err := c.AccountRatedShows(t.Context(), "sess-1", 42, 3); return err }, wantPath: "/3/account/42/rated/tv"},
		{name: "rated episodes", call: func(c *TMDB) error { _, err := c.AccountRatedEpisodes(t.Context(), "sess-1", 42, 3); return err }, wantPath: "/3/account/42/rated/tv/episodes"},
		{name: "watchlist shows", call: func(c *TMDB) error { _, err := c.AccountWatchlist(t.Context(), "sess-1", 42, "show", 3); return err }, wantPath: "/3/account/42/watchlist/tv"},
		{name: "favorite movies", call: func(c *TMDB) error { _, err := c.AccountFavorites(t.Context(), "sess-1", 42, "movie", 3); return err }, wantPath: "/3/account/42/favorite/movies"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubDoer{status: http.StatusOK, body: `{"page":3,"total_pages":3,"results":[]}`}
			c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))
			if err := tt.call(c); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(stub.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(stub.requests))
			}
			req := stub.requests[0]
			if req.Method != http.MethodGet || req.URL.Path != tt.wantPath {
				t.Errorf("request = %s %s, want GET %s", req.Method, req.URL.Path, tt.wantPath)
			}
			q := req.URL.Query()
			if q.Get("session_id") != "sess-1" || q.Get("page") != "3" {
				t.Errorf("query = %v", q)
			}
		})
	}
}

func TestTMDBAccountListDecodes(t *testing.T) {
	t.Parallel()
	t.Run("rated page", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusOK, body: `{"page":1,"total_pages":2,"results":[{"id":603,"rating":7.5,"title":"The Matrix"}]}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "k"}, WithHTTPDoer(stub))
		pg, err := c.AccountRatedMovies(t.Context(), "s", 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if pg.Page != 1 || pg.TotalPages != 2 || len(pg.Results) != 1 || pg.Results[0].ID != 603 || pg.Results[0].Rating != 7.5 {
			t.Errorf("page = %+v", pg)
		}
	})
	t.Run("rated episode page", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusOK, body: `{"page":1,"total_pages":1,"results":[{"id":64782,"show_id":1418,"season_number":7,"episode_number":5,"rating":8}]}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "k"}, WithHTTPDoer(stub))
		pg, err := c.AccountRatedEpisodes(t.Context(), "s", 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Results) != 1 || pg.Results[0] != (TMDBRatedEpisode{ShowID: 1418, Season: 7, Episode: 5, Rating: 8}) {
			t.Errorf("page = %+v", pg)
		}
	})
	t.Run("listed page", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusOK, body: `{"page":1,"total_pages":1,"results":[{"id":1399,"name":"Thrones"}]}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "k"}, WithHTTPDoer(stub))
		pg, err := c.AccountWatchlist(t.Context(), "s", 1, "show", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Results) != 1 || pg.Results[0].ID != 1399 {
			t.Errorf("page = %+v", pg)
		}
	})
}

func TestTMDBAccountListBounds(t *testing.T) {
	t.Parallel()
	stub := &stubDoer{status: http.StatusOK, body: `{}`}
	c := NewTMDB(TMDBCredentials{APIKeyV3: "k"}, WithHTTPDoer(stub))
	for _, page := range []int{0, 501} {
		if _, err := c.AccountRatedMovies(t.Context(), "s", 1, page); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("page %d err = %v, want validation", page, err)
		}
	}
	if _, err := c.AccountFavorites(t.Context(), "s", 1, "episode", 1); !errors.Is(err, httpx.ErrValidation) {
		t.Errorf("episode list err = %v, want validation", err)
	}
	if len(stub.requests) != 0 {
		t.Errorf("requests = %d, want none", len(stub.requests))
	}
}

func TestTMDBAccountListSessionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "401 for the session is a broken session", body: `{"status_code":3}`, want: ErrSessionInvalid},
		{name: "401 for the key is an outage", body: `{"status_code":7}`, want: httpx.ErrProviderUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubDoer{status: http.StatusUnauthorized, body: tt.body}
			c := NewTMDB(TMDBCredentials{APIKeyV3: "k"}, WithHTTPDoer(stub))
			_, err := c.AccountRatedMovies(t.Context(), "sess-1", 1, 1)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "sess-1") {
				t.Errorf("session id leaked into the error: %v", err)
			}
		})
	}
}
