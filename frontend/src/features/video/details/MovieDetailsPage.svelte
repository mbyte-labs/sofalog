<script>
  import { route as routeAction } from '@mateothegreat/svelte5-router'
  import { assetUrl } from '../../../lib/images.js'
  import { TMDB_PATH, IMDB_PATH } from '../../../lib/brandMarks.js'
  import { formatDay, languageName, localDayOf } from '../common/format.js'
  import BrandIcon from '../common/components/BrandIcon.svelte'
  import CastRow from './CastRow.svelte'
  import ChoiceDialog from '../common/components/ChoiceDialog.svelte'
  import DetailHero from '../common/components/DetailHero.svelte'
  import HistoryTimeline from './HistoryTimeline.svelte'
  import MediaGallery from './MediaGallery.svelte'
  import RatingBar from '../common/components/RatingBar.svelte'
  import SimilarRow from './SimilarRow.svelte'
  import WatchLogDialog from '../common/components/WatchLogDialog.svelte'
  import WatchNowSection from './WatchNowSection.svelte'
  // The chrome both detail pages share; see the file's own header for why it
  // is a plain stylesheet rather than a component's scoped style.
  import '../common/detail-page.css'
  import {
    getMovieDetails,
    markWatched,
    addToWatchlist,
    removeFromWatchlist,
    addFavorite,
    removeFavorite,
    addCopy,
    removeCopy,
    rateMovie,
    clearMovieRating,
  } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  // The router remounts this component on every navigation to it, so reading
  // the query once per mount is enough. (A same-page navigation with a
  // different ?tmdb_id would go stale — nothing links that way yet.)
  /** @type {{ route: any }} */
  let { route } = $props()

  // The id travels in the query string; the router parses it off before
  // matching. window.location is the fallback for renders the router did not
  // parameterise (e.g. a status page re-render). Reading the prop's initial
  // value is the point here (once per mount), hence the ignore.
  // svelte-ignore state_referenced_locally
  const qs =
    route?.result?.querystring?.params ??
    Object.fromEntries(new URLSearchParams(window.location.search))
  const tmdbId = qs.tmdb_id
  const localId = qs.id

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'missing' }
   *   | { state: 'error', code: string }
   *   | { state: 'loaded', movie: import('../common/api.js').MovieDetails }}
   */
  let view = $state({ state: 'loading' })

  // Bumped after a write that adds a watch row, to make HistoryTimeline
  // refetch. The timeline picks up a newly minted movie id on its own.
  let historyToken = $state(0)

  if (tmdbId == null && localId == null) {
    view = { state: 'missing' }
  } else {
    getMovieDetails(tmdbId != null ? { tmdbId } : { id: localId })
      .then((movie) => {
        view = { state: 'loaded', movie }
        // The tracking controls initialise from the server's answer, not from
        // local defaults.
        watched = movie.watched
        watchlisted = movie.watchlisted
        favorited = movie.favorited
        userRating = movie.user_rating
        copies = movie.copies
      })
      .catch((err) => {
        view = { state: 'error', code: err instanceof ApiError ? err.code : 'unreachable' }
      })
  }

  // TMDB reports budget and revenue as USD integers (0 = unknown, nulled by
  // the API).
  const moneyFmt = new Intl.NumberFormat('en', {
    style: 'currency',
    currency: 'USD',
    notation: 'compact',
    maximumFractionDigits: 1,
  })

  // The three real tracking controls, initialised from the details response.
  // Watchlist and favorites are each their own axis, never a status (D-011,
  // D-030), hence three independent toggles. Optimistic: flip immediately,
  // revert on failure.
  let watched = $state(false)
  let watchlisted = $state(false)
  let favorited = $state(false)
  let tracking = $state(false)
  /** @type {string | null} one-line failure notice under the buttons */
  let trackError = $state(null)

  /** @param {unknown} err */
  function trackErrorMessage(err) {
    if (err instanceof ApiError && err.code === 'provider_unavailable') {
      return 'TMDB is unreachable right now — try again in a moment. Your library is unaffected.'
    }
    return 'Saving failed — try again.'
  }

  /**
   * Adopt what a tracking call materialised: a page that navigated by tmdb_id
   * has a null local id until the first track action creates the catalog row.
   *
   * @param {{ target_id: number }} res a TrackResult or a RatingResult
   */
  function adoptTracked(res) {
    if (view.state !== 'loaded') return
    view.movie.id = res.target_id
    view.movie.tracked = true
  }

  /** The body markWatched/addToWatchlist need for the current movie. */
  function trackTarget() {
    if (view.state !== 'loaded') return {}
    return view.movie.id != null ? { id: view.movie.id } : { tmdbId: view.movie.tmdb_id }
  }

  /** Whether the "when did you watch it" dialog is up. */
  let logOpen = $state(false)
  /** @type {string | null} a failed save, shown inside the dialog */
  let logError = $state(null)

  // The button never writes directly any more: every watch is dated, and an
  // already-watched movie is still loggable — a rewatch is a new entry
  // (D-009), not a no-op.
  function onMarkWatched() {
    if (view.state !== 'loaded' || tracking) return
    logError = null
    logOpen = true
  }

  /**
   * The dialog was confirmed. Awaited rather than optimistic, unlike the
   * watchlist toggle: a rewatch entry and a rating are real data, and the
   * dialog stays open on failure so the date does not have to be retyped.
   *
   * @param {{ watchedAt: string, rating: number | null }} watch
   */
  async function onLogWatch({ watchedAt, rating }) {
    if (view.state !== 'loaded' || tracking) return
    tracking = true
    logError = null
    try {
      const res = await markWatched({ ...trackTarget(), watchedAt, rating })
      adoptTracked(res)
      watched = true
      if (rating != null) {
        // The same request stored it, so the page's own bar must agree.
        userRating = rating
        view.movie.user_rating = rating
      }
      // The timeline shows watches; refresh it so the new entry appears.
      historyToken++
      logOpen = false
    } catch (err) {
      logError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  /**
   * The timeline removed a watch. The server recomputes status from the log,
   * so the last watch going makes the movie unwatched again — reflect that
   * rather than leaving a ✓ the server no longer agrees with.
   *
   * @param {import('../common/api.js').HistoryEntry} _deleted
   * @param {number | null} remaining watches of this movie still in the log,
   *   or null when a later page might hold more — in which case some remain
   */
  function onWatchDeleted(_deleted, remaining) {
    watched = remaining == null || remaining > 0
  }

  // ── Owned copies (D-021) ──────────────────────────────────────────────

  /** @type {string[]} the kinds this user holds the movie on */
  let copies = $state([])
  /** @type {{ src: { id: string, label: string }, owned: boolean } | null} */
  let copyDialog = $state(null)
  /** @type {string | null} a failed save, shown inside the dialog */
  let copyError = $state(null)
  /**
   * The Jellyfin tile while the library scan owns it (D-033): nothing to
   * confirm, the dialog only says where the answer comes from.
   * @type {{ src: { id: string, label: string }, owned: boolean } | null}
   */
  let copyInfo = $state(null)

  /** The kinds the server's scan manages, which the tiles render read-only. */
  // $derived.by, not $derived: the callback's scope resets narrowing, so
  // `view` keeps its declared union here instead of its initial value.
  const managedKinds = $derived.by(() =>
    view.state === 'loaded' && view.movie.jellyfin_synced ? ['jellyfin'] : [],
  )

  /**
   * A copies tile was tapped: open the confirmation. The write waits for it —
   * a copy is a tracking action, and on an untracked view it materialises the
   * catalog row, so nothing flips optimistically. A scan-managed kind opens
   * the explanation instead: the server refuses a write on it anyway.
   *
   * @param {{ id: string, label: string }} src
   * @param {{ owned: boolean }} s
   */
  function onSelectCopy(src, s) {
    if (view.state !== 'loaded' || tracking) return
    if (managedKinds.includes(src.id)) {
      copyInfo = { src, owned: s.owned }
      return
    }
    copyError = null
    copyDialog = { src, owned: s.owned }
  }

  /** The dialog confirmed. Awaited like the watch log: failure keeps it open. */
  async function onConfirmCopy() {
    if (view.state !== 'loaded' || tracking || copyDialog == null) return
    const { src, owned } = copyDialog
    tracking = true
    copyError = null
    try {
      if (owned) {
        // Only a movie with a catalog row can carry a copy, so the id exists.
        await removeCopy('movie', /** @type {number} */ (view.movie.id), src.id)
        copies = copies.filter((kind) => kind !== src.id)
      } else {
        adoptTracked(await addCopy({ ...trackTarget(), kind: src.id }))
        if (!copies.includes(src.id)) copies = [...copies, src.id]
      }
      copyDialog = null
    } catch (err) {
      copyError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // A linked TMDB account mirrors the change automatically (D-029); the
  // server decides that, so the toggle has nothing to ask.
  async function onToggleWatchlist() {
    if (view.state !== 'loaded' || tracking) return
    tracking = true
    trackError = null
    const was = watchlisted
    watchlisted = !was
    try {
      if (was) {
        // Only a movie with a catalog row can be listed, so the id exists.
        await removeFromWatchlist(/** @type {number} */ (view.movie.id))
      } else {
        adoptTracked(await addToWatchlist(trackTarget()))
      }
    } catch (err) {
      watchlisted = was
      trackError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // The favorites twin of the toggle above (D-030): same optimistic flip,
  // same revert, same materialise-on-add for an untracked movie.
  async function onToggleFavorite() {
    if (view.state !== 'loaded' || tracking) return
    tracking = true
    trackError = null
    const was = favorited
    favorited = !was
    try {
      if (was) {
        // Only a movie with a catalog row can be a favorite, so the id exists.
        await removeFavorite(/** @type {number} */ (view.movie.id))
      } else {
        adoptTracked(await addFavorite(trackTarget()))
      }
    } catch (err) {
      favorited = was
      trackError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  /** @type {number | null} 1–10; RatingBar owns the interaction, not the value */
  let userRating = $state(null)
  /** @type {string | null} a rating failure, shown in place of the bar's caption */
  let rateError = $state(null)

  /**
   * Persist a rating change. Optimistic like the tracking toggles: RatingBar
   * has already moved, so a failure puts the old value back. A cleared rating
   * is a delete, not a low score.
   *
   * @param {number | null} next
   */
  async function onRate(next) {
    if (view.state !== 'loaded' || tracking) return
    // The last value the server acknowledged — what to fall back to.
    const was = view.movie.user_rating
    tracking = true
    rateError = null
    try {
      if (next == null) {
        // Only a movie with a catalog row can carry a rating, so the id exists.
        await clearMovieRating(/** @type {number} */ (view.movie.id))
      } else {
        // Rating is a tracking action: from a live view it materialises the
        // catalog row, whose id the page then adopts.
        adoptTracked(await rateMovie({ ...trackTarget(), rating: next }))
      }
      view.movie.user_rating = next
    } catch (err) {
      userRating = was
      rateError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  const PLACEHOLDER_TIP = 'Coming in a later slice'
</script>

<section class="detail">
  {#if view.state === 'loading'}
    <div class="page-head">
      <h1>Movie details</h1>
    </div>
    <p class="status">Loading…</p>
  {:else if view.state === 'missing' || (view.state === 'error' && view.code === 'not_found')}
    <div class="page-head">
      <h1>Movie details</h1>
    </div>
    <p class="status">
      This movie could not be found.
      <a href="/video/search" use:routeAction>Find it via Search</a>.
    </p>
  {:else if view.state === 'error'}
    <div class="page-head">
      <h1>Movie details</h1>
    </div>
    {#if view.code === 'provider_unavailable'}
      <p class="status error">
        This movie isn't in the library yet and TMDB is unreachable right now — try again later.
        Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Loading failed ({view.code}).</p>
    {/if}
  {:else}
    {@const movie = view.movie}
    <!-- TMDB's official gradient, referenced as fill="url(#tmdb-grad)". -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <DetailHero title={movie.title} poster={movie.poster} posterFull={movie.poster_full}>
      {#snippet info()}
        <h1>{movie.title}</h1>
        {#if movie.director}
          <p class="directed">Directed by {movie.director}</p>
        {/if}
        <p class="meta-line">
          {#if movie.year}
            <span>{movie.year}</span>
          {/if}
          {#if movie.runtime_min != null}
            <span>{movie.runtime_min} min</span>
          {/if}
          <!-- Certification needs the release_dates snapshot — a later slice. -->
          <span class="pending" title={PLACEHOLDER_TIP}>—</span>
          {#if movie.genres.length > 0}
            <span>{movie.genres.join(', ')}</span>
          {/if}
        </p>
      {/snippet}

      {#snippet desc()}
        {#if movie.tagline}
          <p class="tagline">{movie.tagline}</p>
        {/if}
        {#if movie.overview}
          <p class="overview">{movie.overview}</p>
        {:else}
          <p class="overview pending">TMDB has no overview for this movie.</p>
        {/if}
      {/snippet}
    </DetailHero>

    <div class="detail-body">
      <!-- One column at every width, sections in DOM order. -->
      <div class="content">
        <!-- Everything else the snapshot knows: outbound links (the one place
             the app deliberately points away from itself) and TMDB facts. -->
        <div class="section s-details">
          <h2 class="section-label">Details</h2>
          <div class="details">
            <div class="ext-links">
              <a
                class="ext-link"
                href="https://www.themoviedb.org/movie/{movie.tmdb_id}"
                target="_blank"
                rel="noopener noreferrer"
              >
                <BrandIcon path={TMDB_PATH} size={18} fill="url(#tmdb-grad)" />
                TMDB
              </a>
              {#if movie.imdb_id}
                <a
                  class="ext-link"
                  href="https://www.imdb.com/title/{movie.imdb_id}/"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <span class="imdb-logo">
                    <BrandIcon path={IMDB_PATH} size={18} />
                  </span>
                  IMDb
                </a>
              {/if}
              {#if movie.homepage}
                <a class="ext-link" href={movie.homepage} target="_blank" rel="noopener noreferrer">
                  <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                    <circle
                      cx="12"
                      cy="12"
                      r="9"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.8"
                    />
                    <path
                      d="M3 12h18M12 3a13.5 13.5 0 0 1 0 18M12 3a13.5 13.5 0 0 0 0 18"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.8"
                    />
                  </svg>
                  Website
                </a>
              {/if}
              {#if movie.trailer_url}
                <a
                  class="ext-link"
                  href={movie.trailer_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                    <path d="M8 5.5v13l11-6.5z" fill="currentColor" />
                  </svg>
                  Trailer
                </a>
              {/if}
            </div>

            {#if movie.original_language || movie.budget != null || movie.revenue != null || movie.popularity != null}
              <div class="facts">
                {#if movie.original_language}
                  <div class="fact">
                    <span class="fact-label">Language</span>
                    <span class="fact-value">{languageName(movie.original_language)}</span>
                  </div>
                {/if}
                {#if movie.budget != null}
                  <div class="fact">
                    <span class="fact-label">Budget</span>
                    <span class="fact-value">{moneyFmt.format(movie.budget)}</span>
                  </div>
                {/if}
                {#if movie.revenue != null}
                  <div class="fact">
                    <span class="fact-label">Revenue</span>
                    <span class="fact-value">{moneyFmt.format(movie.revenue)}</span>
                  </div>
                {/if}
                {#if movie.popularity != null}
                  <div class="fact">
                    <span class="fact-label">Popularity</span>
                    <span class="fact-value">{movie.popularity.toFixed(1)}</span>
                    <!-- Popularity is TMDB's trending metric — only as current
                         as the snapshot, so date it. -->
                    <span class="fact-note">as of {formatDay(localDayOf(movie.fetched_at))}</span>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>

        <!-- External scores and the user's own rating are different things,
             so they get separate labelled cards sharing one row on wide
             screens (stacking below it). -->
        <div class="section s-ratings">
          <div class="ratings-row">
            <div class="ratings-block">
              <h2 class="section-label">Ratings</h2>
              <div class="scores">
                <div class="score">
                  <span class="score-logo">
                    <BrandIcon path={TMDB_PATH} size={36} fill="url(#tmdb-grad)" label="TMDB" />
                  </span>
                  <span class="score-value">
                    {movie.rating != null ? movie.rating.toFixed(1) : '—'}
                  </span>
                  {#if movie.votes != null}
                    <span class="score-votes">{movie.votes.toLocaleString()} votes</span>
                  {:else}
                    <span class="score-votes pending">no votes</span>
                  {/if}
                </div>
                <div class="score-divider" aria-hidden="true"></div>
                <div class="score">
                  <span class="score-logo imdb-logo">
                    <BrandIcon path={IMDB_PATH} size={36} label="IMDb" />
                  </span>
                  <span class="score-value pending" title={PLACEHOLDER_TIP}>—</span>
                  <span class="score-votes pending">coming soon</span>
                </div>
              </div>
            </div>

            <div class="ratings-block rate-block">
              <h2 class="section-label">Your rating</h2>
              <RatingBar
                bind:value={userRating}
                onrate={onRate}
                disabled={tracking}
                note={rateError}
              />
            </div>
          </div>
        </div>

        <WatchNowSection
          {copies}
          {managedKinds}
          streaming={movie.streaming}
          subject="movie"
          disabled={tracking}
          onselect={onSelectCopy}
        />

        <!-- The target filter needs the LOCAL id, which a tmdb_id navigation
             only knows after the details response — and after the first track
             action on a live view. The timeline loads when it arrives. -->
        <HistoryTimeline
          targetType="movie"
          targetId={movie.id}
          reloadToken={historyToken}
          emptyNote="You have not watched this movie yet."
          ondeleted={onWatchDeleted}
        />

        <div class="section s-actions">
          <div class="actions">
            <!-- Enabled even when watched: the ✓ treatment is the state, and
                 pressing it again logs another dated entry. -->
            <button
              class="watch-cta"
              class:done={watched}
              onclick={onMarkWatched}
              disabled={tracking}
              aria-pressed={watched}
              title={watched ? 'Log another watch of this movie' : 'Log a watch of this movie'}
            >
              {watched ? 'Log another watch' : 'Mark As Watched'}
            </button>
            <button
              class="icon-toggle"
              class:on={watchlisted}
              onclick={onToggleWatchlist}
              disabled={tracking}
              aria-pressed={watchlisted}
              aria-label={watchlisted ? 'Remove from watchlist' : 'Add to watchlist'}
              title={watchlisted ? 'Remove from watchlist' : 'Add to watchlist'}
            >
              <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
                <path
                  d="M7 4h10a1 1 0 0 1 1 1v15l-6-4-6 4V5a1 1 0 0 1 1-1z"
                  fill={watchlisted ? 'currentColor' : 'none'}
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linejoin="round"
                />
                {#if !watchlisted}
                  <path
                    d="M12 8v5M9.5 10.5h5"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                  />
                {/if}
              </svg>
            </button>
            <!-- The third axis (D-030): a filled heart when favorited, an
                 outline when not. -->
            <button
              class="icon-toggle"
              class:on={favorited}
              onclick={onToggleFavorite}
              disabled={tracking}
              aria-pressed={favorited}
              aria-label={favorited ? 'Remove from favorites' : 'Add to favorites'}
              title={favorited ? 'Remove from favorites' : 'Add to favorites'}
            >
              <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
                <path
                  d="M12 20.5l-1.3-1.2C6.1 15.2 3 12.4 3 8.9 3 6.1 5.2 4 8 4c1.6 0 3.1.7 4 1.9C12.9 4.7 14.4 4 16 4c2.8 0 5 2.1 5 4.9 0 3.5-3.1 6.3-7.7 10.4L12 20.5z"
                  fill={favorited ? 'currentColor' : 'none'}
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
          {#if trackError}
            <p class="demo-hint" role="alert">{trackError}</p>
          {/if}
        </div>

        <div class="section s-cast">
          <h2 class="section-label">Cast</h2>
          <CastRow
            cast={movie.cast}
            title={movie.title}
            emptyNote="TMDB lists no cast for this movie."
          />
        </div>

        <div class="section s-media">
          <h2 class="section-label">Media</h2>
          <MediaGallery
            title={movie.title}
            videos={movie.videos}
            backdrops={movie.backdrops}
            posters={movie.posters}
            emptyNote="TMDB lists no media for this movie."
          />
        </div>

        <!-- TMDB's recommendations, out of the same snapshot as everything
             above (D-024): a tracked movie keeps its rail with TMDB down. Every
             card is a title the library has no row for, reached by tmdb_id. -->
        <div class="section s-similar">
          <h2 class="section-label">Similar movies</h2>
          <SimilarRow
            items={movie.similar}
            kind="movie"
            emptyNote="TMDB lists no similar movies."
          />
        </div>
      </div>
    </div>

    {#if logOpen}
      <WatchLogDialog
        title={movie.title}
        busy={tracking}
        error={logError}
        initialRating={userRating}
        onconfirm={onLogWatch}
        oncancel={() => (logOpen = false)}
      />
    {/if}

    {#if copyDialog}
      <ChoiceDialog
        heading={copyDialog.owned
          ? `Remove ${copyDialog.src.label} copy?`
          : `Add ${copyDialog.src.label} copy?`}
        subject={movie.title}
        actions={[
          {
            id: copyDialog.owned ? 'remove' : 'add',
            label: copyDialog.owned
              ? `Remove ${copyDialog.src.label} copy`
              : `Add ${copyDialog.src.label} copy`,
          },
        ]}
        busy={tracking}
        error={copyError}
        onconfirm={onConfirmCopy}
        oncancel={() => (copyDialog = null)}
      />
    {/if}

    {#if copyInfo}
      <!-- The scan's tile (D-033): the answer is the server's, so the one
           action is the way to the scan's page. -->
      <ChoiceDialog
        heading={copyInfo.owned ? `On ${copyInfo.src.label}` : `Not on ${copyInfo.src.label}`}
        subject={movie.title}
        note="This tile mirrors your Jellyfin server's library — it updates on the next scan and can't be set by hand."
        actions={[{ id: 'jellyfin', label: 'Jellyfin scan settings', href: '/settings/jellyfin' }]}
        cancelLabel="Close"
        oncancel={() => (copyInfo = null)}
      />
    {/if}
  {/if}
</section>
