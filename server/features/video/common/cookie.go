package common

// The TMDB account link (D-029) and its setup (D-036). Two of the link routes are BROWSER
// NAVIGATION, not API calls: start sends the user to TMDB's approval page and
// callback is where TMDB sends them back, so both answer with redirects — an
// error envelope in the address bar is a dead end. The same shape as the OIDC
// round trip in auth, with its own cookie: features never import each other
// (D-014), so the flow-cookie helpers are copied, not shared.

// CookieSettings are the attributes video's own cookies carry, handed in from
// main so BASE_URL stays the one source of the path and the Secure flag.
type CookieSettings struct {
	Path   string
	Secure bool
}
