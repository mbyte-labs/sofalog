package common

// Image size variants — TMDB provider variants, never resized locally. Every
// card and page DTO carries both (AssetLinker.Poster / Backdrop), the SPA
// offers both through srcset and the browser picks by screen density and
// rendered width; only the media lightbox pins the full size.
// Posters (2:3) and backdrops (16:9) have separate variant ladders at TMDB, so
// they get separate pairs rather than one shared size.
//
// Profiles (a person's portrait) have a third ladder — w45/w185/h632 — so the
// person page's full portrait is h632, not w500; its thumb is the w185 every
// cast photo already uses. An item's cast photos stay single-size w185 in
// data/assets (SPEC §5); the person page owns no assets and both of its
// variants ride the cache proxy only (D-027).
const (
	PosterSizeThumb = "w185"
	PosterSizeFull  = "w500"

	BackdropSizeThumb = "w300"
	BackdropSizeFull  = "w1280"

	ProfileSizeFull = "h632"
)
