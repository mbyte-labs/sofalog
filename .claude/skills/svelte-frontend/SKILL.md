---
name: svelte-frontend
description: Use when working on the sofalog Svelte SPA under frontend/ — the per-feature directories (src/features/auth|video|games with their routes, components and stores), the shared /api/v1 fetch client in src/lib, the PWA manifest and service worker, vite.config.js, or the static build that gets embedded into the Go binary. Covers relative asset paths, session-cookie fetch rules and 401 handling, thumbnail-versus-full image selection, optimistic episode marking and bulk actions, derived progress display, responsive layout and touch support (breakpoints, hover-none ergonomics, safe areas, mobile/tablet/desktop), and the prettier / svelte-check gate.
---

# sofalog Svelte SPA

Plain Svelte + Vite, **SPA mode — not SvelteKit**. Built to static files and embedded into the Go
binary. No Node runtime in production.

The bundled UI is the **reference client of the public API**. If a screen needs something the API
cannot express, fix the API — do not reach around it. That dogfooding is why Svelte was chosen over
server-rendered HTML.

## Layout — feature-first (D-014, D-037)

Code is grouped by feature, mirroring the backend's packages: `auth`, `settings`, and the `video`
component split per feature exactly like `server/features/video/` (D-037). A feature's routes,
components, stores and API-call modules live in its own directory; `src/lib/` holds **only**
genuinely cross-component code.

```
frontend/
├── vite.config.js
├── src/
│   ├── main.js
│   ├── App.svelte              shell + router outlet (maps paths to feature routes)
│   ├── features/
│   │   ├── auth/               login route, session store
│   │   ├── settings/           settings pages (Data Sources, TMDB account, Jellyfin, restore)
│   │   ├── video/              movies & TV, one directory per feature:
│   │   │   ├── common/         api.js (every /video endpoint), format.js, posterPlaceholder.js,
│   │   │   │                   ratingColor.js, detail-page.css, components/ used by 2+ features
│   │   │   │                   (BrandIcon, ChoiceDialog, CreditCard, DetailHero, RatingBar,
│   │   │   │                   WatchLogDialog)
│   │   │   ├── upnext/ upcoming/ history/ lists/ search/ trending/ details/ people/
│   │   │   │                   each: its page(s) plus the components and helpers only it uses
│   │   └── games/              (M4)
│   ├── lib/
│   │   ├── api/client.js       the fetch wrapper — the ONLY place fetch() appears;
│   │   │                       features build their calls on top of it
│   │   ├── components/         reusable cross-feature UI only
│   │   └── images.js           the single asset-URL helper
│   └── app.css
└── public/                     manifest.json, icons, service worker
```

If something is used by exactly one feature, it belongs in that feature's directory, not in `lib/`
or `video/common/`. A video feature imports from `video/common/` and `src/lib/`, **never from a
sibling feature directory** — the moment a second feature needs a component, move it to
`common/components/` (the Go side's rule, D-037).

## Vite invariants

```js
export default defineConfig({
  base: './',                        // RELATIVE. Absolute paths break behind a sub-path proxy.
  build: {
    outDir: '../server/web/dist',
    emptyOutDir: false,              // must NOT delete the committed .gitkeep / stub index.html
  },
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
})
```

- `base: './'` is a hard requirement — a self-hosted deployment may be served under a sub-path, and
  absolute `/assets/...` URLs baked into the bundle break it. Never write an absolute app URL in
  source either.
- `emptyOutDir: false` matters because `//go:embed all:dist` **fails to compile** against a missing or
  empty directory. The build output is gitignored except a committed `.gitkeep` and a stub
  `index.html`, so a fresh clone can `go build` before anyone runs Vite. Wiping the dir breaks that.

## The API client

Exactly one module wraps `fetch`. Components import from it and never call `fetch` themselves.

```js
// lib/api/client.js
async function request(method, path, body) {
  const res = await fetch(`api/v1${path}`, {   // relative — no leading slash, matches base: './'
    method,
    credentials: 'include',                     // session cookie auth
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401) {
    session.set(null)
    goto('/login')                              // ONE central place handles auth expiry
    throw new ApiError('unauthorized', 'session expired', 401)
  }
  if (!res.ok) {
    const payload = await res.json().catch(() => null)
    // Backend envelope: { error: { code, message } }
    throw new ApiError(payload?.error?.code ?? 'unknown', payload?.error?.message ?? res.statusText, res.status)
  }
  return res.status === 204 ? null : res.json()
}
```

- `credentials: 'include'` on every call — auth is an HTTP-only cookie, so there is no token for JS
  to attach (and JS must not be able to read it).
- Branch on `error.code`, never on `error.message`.
- A 503 with a provider code is expected on **search, add, the person page and the Trending page**
  (the always-live discovery views) and should render as "provider unavailable, your library is
  unaffected". A 503 anywhere else is a backend bug worth reporting, not
  something to paper over in the UI.

## Images: offer both variants, let the browser pick

The API returns **relative** asset paths at **both** provider sizes — `poster` (w185) beside
`poster_full` (w500), `backdrop` / `backdrop_full` (w300 / w1280), `still` / `still_full`, the
Lists hub's parallel `backdrops` / `backdrops_full`. `lib/images.js` is the one place that turns
them into URLs and srcsets:

```js
// lib/images.js
export function assetUrl(path) {
  return path ? path.replace(/^\/+/, '') : PLACEHOLDER
}
export function posterSrcset(thumb, full) {
  return thumb && full ? `${assetUrl(thumb)} 185w, ${assetUrl(full)} 500w` : undefined
}
export const GRID_POSTER_SIZES = '(max-width: 767px) 45vw, 260px'
```

```svelte
<img
  src={assetUrl(entry.poster)}
  srcset={posterSrcset(entry.poster, entry.poster_full)}
  sizes={GRID_POSTER_SIZES}
  alt=""
  loading="lazy"
/>
```

**The rule:** every poster, backdrop and still `<img>` keeps the thumb as `src`, offers the pair
through `srcset` (`posterSrcset` / `backdropSrcset`) and states its rendered width through
`sizes`; the **browser** picks by device pixel ratio × that width — a component never chooses a
variant itself. The mistake to avoid is a `srcset` **without `sizes`**: the browser then assumes
the full viewport width and every grid card fetches the w500 for nothing. Reuse the shared
`*_SIZES` constants when a card renders at a known width; the lightbox alone pins the full size.
Cast photos are single-size and get no srcset. Always handle a missing image: non-Steam games
legitimately have no cover.

## Desktop, tablet and phone — every screen ships for all three

Phones, tablets and desktops are all first-class, and the video feature skews **phone-heavy** in
practice. This is met by **one responsive SPA** — never device detection, never a forked component
or route per device class. A screen that only works with a mouse, or only fits a phone, is
unfinished.

**Widths — three breakpoints and no others.** The scale is documented in a comment at the top of
`src/app.css` (custom properties cannot appear in media queries, so the comment is the token):

| Width | Meaning |
|---|---|
| `max-width: 599px` | phone — filter bars stack, dialogs become bottom sheets, tighter grids |
| `max-width: 767px` | drawer — the sidebar becomes the off-canvas drawer; `--space-page`/`--poster-min` shrink |
| `min-width: 1100px` | wide — detail-page two-column hero, larger media tiles |

Do not invent a fourth width. Tablets are not a special case: they get the phone or desktop layout
depending on which side of these lines they land, plus the touch rules below via `(hover: none)`.

**Capability, not width, decides input ergonomics.** Touch adjustments live in `@media (hover:
none)` blocks, per component, so a touch laptop and a tablet get them at any width:

- **≥44px tap targets** (`2.75rem`, the `.menu-button` size) on every control a thumb hits. When a
  44px visual would overwhelm the design (a small badge-sized control over artwork), keep the visual
  smaller and extend the hit area with an invisible `::after { inset: -0.35rem }` halo.
- **Neutralize every hover lift** (`transform: none` etc.) — a hover effect on touch reads as stuck
  state after a tap. And never make an affordance hover-*revealed*: controls stay visible always and
  only re-color on hover (the history timeline's ✕ is the model).
- The **16px form-control floor** is a single global guard in `app.css` (`font-size: max(1rem, 1em)
  !important` under `(hover: none)`) — anything smaller makes iOS Safari zoom the page on focus.
  Don't re-declare it per component, and don't fight it: it is `!important` because Svelte's scoped
  selectors would otherwise out-rank it.

**Layout reflows from tokens, not per-page pixel math.** Grids are
`repeat(auto-fill, minmax(var(--poster-min), 1fr))`; page padding is `var(--space-page)`. Both
shrink once, in `app.css`'s 767px block, and every page follows. A page-specific track minimum
(the episode grid's `--ep-min`) is fine, but it adjusts at the shared breakpoints.

**iOS realities**, all already wired — keep them intact when touching the shell or overlays:

- `100dvh`, never `100vh` (the URL-bar problem). `viewport-fit=cover` is set in `index.html`, so
  full-bleed elements pad with `max(var(--space-page), env(safe-area-inset-*))` /
  `calc(… + env(safe-area-inset-bottom))` — `App.svelte`'s `<main>` is the reference.
- Overlays lock body scroll while open (save and **restore** the previous value) and carry
  `overscroll-behavior: contain`; horizontal rails carry `overscroll-behavior-x: contain` so an
  end-of-rail swipe doesn't become browser back-navigation.
- A form dialog becomes a **bottom sheet** at ≤599px so its confirm button sits under the thumb —
  `WatchLogDialog.svelte` is the app's only form modal and the template for any future one.

**PWA icons are a five-file set**: `logo.svg` plus rasterized `icon-192.png`, `icon-512.png`,
`maskable-512.png` (art inside the central 80% safe zone, full-bleed tile background) and
`apple-touch-icon.png` (opaque — iOS ignores manifest icons entirely and renders transparency as
black; only the `<link rel="apple-touch-icon">` in `index.html` reaches it). If the logo changes,
regenerate all four PNGs and bump `sw.js`'s cache key so installed clients pick them up.

**Verify both modes before calling UI work done**: browser device emulation at 375px width with
touch emulation on (that is what makes `(hover: none)` match) for the phone pass, and an untouched
desktop pass — the touch blocks must change nothing there.

## Progress *and* status are computed on the server

Show and season progress, the "next unwatched episode", **and the watch status itself** come from the
API as computed values. Do not recompute them client-side from a partial episode list, and never set a
status locally after marking something — the server recomputes it (marking the last episode of a show
flips it to *watched* and drops the card out of "Currently watching"), and its answer is the only
correct one.

Progress arrives as three numbers: `watched`, `aired_total`, `total`. **Render `watched / aired_total`**,
with `total` secondary. Completion counts only aired episodes, so showing `8/10` beside a *watched*
badge reads as a bug.

## Optimistic marking and bulk actions

Marking must feel instant, and bulk actions (mark season, mark whole show) can
touch hundreds of episodes.

```
1. Apply the change to the store immediately; snapshot the previous value.
2. Fire the API call.
3. On success: replace local state with the SERVER response (it carries recomputed progress).
4. On failure: restore the snapshot and surface the error.
```

- One request per bulk action — call the bulk endpoint. Never loop `fetch` per episode; that defeats
  the server's transaction and hammers the API.
- Server response is truth, and it now carries the recomputed **status** as well as progress. Do not
  merge optimistic state with the response field-by-field: replace.
- **Retries and double taps must go to the idempotent mark endpoint, never to "log a rewatch".**
  Marking watched is idempotent server-side; logging a rewatch always inserts. Wiring a retry to the
  wrong one silently doubles every row in a bulk action.
- Logging a rewatch is a **separate, explicit affordance** — never the same tap target as marking
  watched. Tapping an already-watched episode must not silently create a second watch.
- If a mark response comes back with `status: "dropped"`, the backend deliberately kept it dropped
  (it is sticky). Offer an explicit "resume this show?" prompt rather than hiding the discrepancy.
- Confirm before anything that could orphan an item — untracking triggers the deletion flow
  (D-008), which needs a confirmation step, not a silent success toast. The confirmation should say
  how many watch entries will be removed, since untracking a show deletes this user's watch history
  for it.

## PWA and the offline story

`manifest.json` (name, icons, `display: standalone`) plus a minimal service worker. HTTPS is required
for the service worker, which the reverse proxy provides.

Service worker rules:

- Cache the **app shell** (`index.html`, JS, CSS) and `/assets/` images. Network-first, cache as
  fallback.
- **Never cache `/api/v1` mutations**, and do not serve stale JSON for reads that drive marking
  actions.
- Provide an offline fallback page.

**Read D-004 before building anything offline-shaped.** The requirement that "with no internet access
at all, everything except adding items behaves normally" is about **provider** outages, not
client↔server connectivity — a self-hosted app on a LAN still reaches its own server. So: **no
IndexedDB mirror of the library, no client-side write queue, no sync or conflict-resolution layer.**
Mutations require the server. Building otherwise turns a CRUD app into a distributed system with a
merge problem.

## Domain rules the UI must respect

- **Watchlist is a separate toggle, never a value in the status control.** Statuses are
  *watching / watched / dropped*; "want to watch" is its own independent axis for movies and shows
  (D-011). An item can be watchlisted **and** in progress — the UI must allow both at once.
- **Seasons are NOT rateable** (D-020) — movies, shows and episodes are. The season page's "Your
  rating" slot shows the average of the user's episode ratings with a rated-of-total count, never a
  rating control.
- **History by date can legitimately repeat a title** — rewatches are real entries, not duplicates to
  deduplicate away.
- **Games have no play status control.** Played/unplayed is derived from hours (0 = unplayed). The
  library page shows **cover art and name only**; everything else is on the detail page.
- **No manual-entry form for movies or shows** — TMDB search is the only path. Manual entry *is*
  first-class for game hours and achievements.
- Ratings are integers 1–10 everywhere, including on individual episodes. Re-rating overwrites; there
  is no rating history to display.
- **The TMDB attribution notice must be visible in the UI**: "This product uses the TMDB API but is
  not endorsed or certified by TMDB". It lives on **Settings → Data Sources and nowhere else** — that
  page is the single home for every provider disclosure, and it is permanent (the sidebar's account
  chip links to Settings unconditionally). Do **not** add the notice, or any other provider disclaimer, to an
  individual page — not even one that renders a provider's logo. Explain the provider in the page's
  own words and link to `/settings/data-sources`. A second instance is a per-case call the user
  makes explicitly.

## Gate

```
cd frontend && npm run check && npm run lint
```

`svelte-check` and prettier. Prettier also runs automatically on edited frontend files via the
`PostToolUse` hook once `frontend/node_modules` exists. Adding an npm package requires asking first.
