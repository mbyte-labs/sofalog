package common

import "regexp"

// YouTubeKeyRe matches a YouTube video id: exactly eleven base64url characters,
// which is what YouTube has issued since it started and what TMDB stores. The
// key becomes a URL path element upstream and a filename in the cache, so the
// shape is whitelisted rather than sanitised — nothing matching this can form a
// separator, a dot-run or a query string.
var YouTubeKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
