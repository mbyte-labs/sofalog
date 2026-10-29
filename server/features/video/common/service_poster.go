package common

import "regexp"

// PosterFileRe matches a TMDB image filename: one plain path element, image
// extension only. Whitelisting the shape (rather than blacklisting "..") is
// what rules out traversal — anything with a separator or a dot-run simply
// does not match. "-" and "_" are in there because TMDB's filenames are
// base64url-ish hashes: rejecting them turned a legitimate image into a 422,
// which a gallery of sixty files makes far likelier to hit than a single poster
// did. Neither character can form a separator or a dot-run, so the guarantee
// is unchanged.
var PosterFileRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(jpg|jpeg|png)$`)
