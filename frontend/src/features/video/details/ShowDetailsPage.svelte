<script>
  // Show details. Like the movie page this serves a stored snapshot once the
  // show is tracked and goes live to TMDB when it is not (D-015) — so a tracked
  // show keeps rendering with TMDB down, and only an untracked one can 503.
  //
  // Everything the movie page and this one render the same way is shared:
  // ./components/ for the hero, rating bar, watch-now tiles and media gallery,
  // ./detail-page.css for the chrome. What is here is what is genuinely
  // show-shaped — the season grid, and the facts TV has that film does not.
  //
  // "Mark As Watched" here is a BULK action: it marks every aired, non-special
  // episode. It opens WatchLogDialog in its bulk mode rather than writing
  // straight away — the dialog asks which episodes (remaining only, or a full
  // rewatch) and where each row's date comes from (now, a fixed instant, or
  // the episode/season release dates) — and it is awaited rather than flipped
  // optimistically, because the first mark also downloads the whole show.
  //
  // The watchlist beside it is the other axis, never a status (D-011): a show
  // can be listed and already finished at once. Listing an untracked show pulls
  // it local too, which is why the hint below the buttons names both.
  //
  // Dropping lives with the progress line instead of up here, because it is the
  // one thing on the page that SETS the status the pill shows (D-010) rather
  // than recording a watch — and because it only exists once the show has been
  // started, where the two buttons above are always available.

  import { route as routeAction } from '@mateothegreat/svelte5-router'
  import { assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { TMDB_PATH, IMDB_PATH } from '../../../lib/brandMarks.js'
  import { formatDay, languageName, localDayOf, todayLocal } from '../common/format.js'
  import {
    getShowDetails,
    markShowWatched,
    addToWatchlist,
    removeFromWatchlist,
    addFavorite,
    removeFavorite,
    dropShow,
    undropShow,
    addCopy,
    removeCopy,
    rateTarget,
    clearRating,
  } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'
  import BrandIcon from '../common/components/BrandIcon.svelte'
  import CastRow from './CastRow.svelte'
  import ChoiceDialog from '../common/components/ChoiceDialog.svelte'
  import DetailHero from '../common/components/DetailHero.svelte'
  import EpisodeAverage from './EpisodeAverage.svelte'
  import HistoryTimeline from './HistoryTimeline.svelte'
  import MediaGallery from './MediaGallery.svelte'
  import RatingBar from '../common/components/RatingBar.svelte'
  import SimilarRow from './SimilarRow.svelte'
  import WatchLogDialog from '../common/components/WatchLogDialog.svelte'
  import WatchNowSection from './WatchNowSection.svelte'
  import '../common/detail-page.css'

  /** @type {{ route: any }} */
  let { route } = $props()

  // The id travels in the query string, read once per mount — the router
  // remounts the component on navigation, so there is nothing to react to.
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
   *   | { state: 'loaded', show: import('../common/api.js').ShowDetails }}
   */
  let view = $state({ state: 'loading' })

  /**
   * Load the show and seed every control from the server's answer. Also the
   * re-read after a delete changes derived state — see onWatchDeleted.
   *
   * @param {boolean} [initial] false when refreshing an already-rendered page,
   *   which must not fall back to an error screen over a transient failure
   * @returns {Promise<void>} settles when the view is up to date (or kept)
   */
  function loadShow(initial = true) {
    // A refresh keeps whatever local id the page has adopted since mount.
    const addressed =
      view.state === 'loaded' && view.show.id != null
        ? { id: view.show.id }
        : localId != null
          ? { id: localId }
          : { tmdbId }
    return getShowDetails(addressed)
      .then((show) => {
        view = { state: 'loaded', show }
        watched = show.watched
        watchlisted = show.watchlisted
        favorited = show.favorited
        userRating = show.user_rating
        status = show.status
        progress = show.progress
      })
      .catch((err) => {
        // A failed refresh leaves the page as it was: the figures are one
        // action stale, which beats replacing a working page with an error.
        if (initial) {
          view = { state: 'error', code: err instanceof ApiError ? err.code : 'unreachable' }
        }
      })
  }

  if (tmdbId == null && localId == null) {
    view = { state: 'missing' }
  } else {
    loadShow()
  }

  /** @type {number | null} this user's rating, 1-10 */
  let userRating = $state(null)
  let watched = $state(false)
  let watchlisted = $state(false)
  let favorited = $state(false)
  /** @type {'watching'|'watched'|'dropped'|null} */
  let status = $state(null)
  /** @type {import('../common/api.js').ShowProgress | null} */
  let progress = $state(null)
  // One in-flight flag across marking and rating, as on the movie page: one
  // action disables the others rather than racing them.
  let tracking = $state(false)
  /** @type {string | null} one-line failure notice under the buttons */
  let trackError = $state(null)
  /** @type {string | null} rating failure, shown by the bar itself */
  let rateError = $state(null)

  const PLACEHOLDER_TIP = 'Coming in a later slice'

  const STATUS_LABELS = {
    watching: 'Watching',
    watched: 'Watched',
    dropped: 'Dropped',
  }

  /** @param {unknown} err */
  function trackErrorMessage(err) {
    if (err instanceof ApiError && err.code === 'provider_unavailable') {
      return 'TMDB is unreachable right now — try again in a moment. Your library is unaffected.'
    }
    return 'Saving failed — try again.'
  }

  // The local id once the show is tracked, else its tmdb_id — the same "adopt
  // the id a write minted" pattern the movie page uses.
  function trackTarget() {
    if (view.state !== 'loaded') return {}
    return view.show.id != null ? { id: view.show.id } : { tmdbId: view.show.tmdb_id }
  }

  // Whether the bulk dialog is up, and a failed save shown inside it — the
  // movie and episode pages' logOpen/logError pattern.
  let logOpen = $state(false)
  /** @type {string | null} */
  let logError = $state(null)

  // No `watched` guard: a finished show can be logged again — the dialog's
  // rewatch scope is what makes the button stop being a dead end.
  function onMarkWatched() {
    if (view.state !== 'loaded' || tracking) return
    logError = null
    logOpen = true
  }

  /**
   * The dialog confirmed. Awaited, never optimistic: the first call
   * materialises every season and episode, which takes seconds and can
   * genuinely fail — and the response carries the recomputed status and
   * progress, which replace the page's copies wholesale.
   *
   * @param {{ rewatch: boolean, mode: 'now'|'fixed'|'episode_air'|'season_air',
   *   watchedAt: string | null }} choice
   */
  async function onBulkLog({ rewatch, mode, watchedAt }) {
    if (view.state !== 'loaded' || tracking) return
    const show = view.show
    tracking = true
    logError = null
    try {
      const res = await markShowWatched({ ...trackTarget(), rewatch, mode, watchedAt })
      show.id = res.target_id
      show.tracked = true
      status = res.status
      progress = res.progress
      watched = res.status === 'watched'
      historyToken++
      logOpen = false
    } catch (err) {
      logError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // Awaited like the mark above, not optimistic: adding an untracked show
  // materialises the whole show and downloads its media set, which takes
  // seconds and can genuinely fail. Removing is local and instant, but shares
  // the one in-flight flag so the two buttons cannot race. A linked TMDB
  // account mirrors the change automatically (D-029); the server decides that.
  async function onToggleWatchlist() {
    if (view.state !== 'loaded' || tracking) return
    const show = view.show
    tracking = true
    trackError = null
    try {
      if (watchlisted) {
        // Only a show with a catalog row can be listed, so the id exists.
        await removeFromWatchlist(/** @type {number} */ (show.id), 'show')
        watchlisted = false
      } else {
        const res = await addToWatchlist({ targetType: 'show', ...trackTarget() })
        show.id = res.target_id
        show.tracked = true
        watchlisted = true
      }
    } catch (err) {
      trackError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // The favorites twin of the toggle above (D-030): awaited for the same
  // reason — an untracked show is materialised whole on the way in.
  async function onToggleFavorite() {
    if (view.state !== 'loaded' || tracking) return
    const show = view.show
    tracking = true
    trackError = null
    try {
      if (favorited) {
        // Only a show with a catalog row can be a favorite, so the id exists.
        await removeFavorite(/** @type {number} */ (show.id), 'show')
        favorited = false
      } else {
        const res = await addFavorite({ targetType: 'show', ...trackTarget() })
        show.id = res.target_id
        show.tracked = true
        favorited = true
      }
    } catch (err) {
      trackError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  /** @param {number | null} value */
  async function onRate(value) {
    if (view.state !== 'loaded' || tracking) return
    tracking = true
    rateError = null
    const previous = userRating
    userRating = value
    try {
      if (value == null) {
        const id = view.show.id
        if (id != null) await clearRating('show', id)
      } else {
        const res = await rateTarget({ targetType: 'show', ...trackTarget(), rating: value })
        view.show.id = res.target_id
        view.show.tracked = true
      }
    } catch (err) {
      userRating = previous
      rateError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // Bumped after a write that adds watch rows, to make HistoryTimeline
  // refetch — a bulk mark writes hundreds at once.
  let historyToken = $state(0)

  /**
   * The timeline removed a watch. Both figures this page shows are derived
   * from the log, so losing an episode's last watch moves them: progress drops
   * by one, and the status can fall back from `watched` to `watching` or
   * collapse entirely — while a user-set `dropped` sticks.
   *
   * Re-read rather than recompute. Mirroring `recomputeShowStatus` here would
   * duplicate a state machine that is deliberately server-owned (D-010) and
   * drift from it; one request cannot disagree with the server.
   *
   * @param {import('../common/api.js').HistoryEntry} _deleted
   * @param {number | null} remaining watches of that episode still loaded, or
   *   null when a later page might hold more
   */
  function onWatchDeleted(_deleted, remaining) {
    // Progress counts episodes, not watch rows, so removing one of several
    // rewatches changes nothing derived and needs no round trip.
    if (remaining === 0) loadShow(false)
  }

  // ── Dropping (D-010) ─────────────────────────────────────────────────

  // 'dropped' is the one status the user sets by hand, and it is sticky: no
  // later watch, no newly aired episode and no recompute clears it — only an
  // explicit un-drop does. Both directions confirm first: the drop because it
  // silently removes the show from Up Next, the un-drop because it recomputes.
  let dropOpen = $state(false)
  /** @type {string | null} a failed save, shown inside the dialog */
  let dropError = $state(null)

  function onToggleDrop() {
    if (view.state !== 'loaded' || tracking) return
    dropError = null
    dropOpen = true
  }

  /**
   * The dialog confirmed. Awaited and never optimistic, like every other write
   * on this page, and the answer REPLACES both figures rather than being
   * guessed: an un-drop recomputes from the watch log, so whether the status
   * lands on watched, watching or nothing at all is the server's to state
   * (D-010) — the same reason the bulk mark reads its status off the response.
   *
   * No loadShow() afterwards: DropResult already carries everything a drop can
   * change.
   */
  async function onConfirmDrop() {
    if (view.state !== 'loaded' || tracking) return
    // Only a show with a state row shows the button, and a state row implies a
    // catalog row, so the local id exists.
    const id = /** @type {number} */ (view.show.id)
    const dropped = status === 'dropped'
    tracking = true
    dropError = null
    try {
      const res = dropped ? await undropShow(id) : await dropShow(id)
      status = res.show.status
      progress = res.show.progress
      watched = status === 'watched'
      dropOpen = false
    } catch (err) {
      dropError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // ── Seasons grid ─────────────────────────────────────────────────────

  // A long-running show has more seasons than anyone scrolls past on the way
  // to the rest of the page, so the grid opens with one screenful and the rest
  // is one click away. The API returns them all — how many to show is
  // presentation, and only the client knows how much room it has.
  const SEASON_PAGE = 20
  let allSeasons = $state(false)

  // $derived.by, not $derived: a bare expression is evaluated in the module
  // body, where the control flow above has already narrowed `view` away from
  // 'loaded'. A callback sees the declared union.
  const seasons = $derived.by(() => (view.state === 'loaded' ? view.show.seasons : []))
  const visibleSeasons = $derived(allSeasons ? seasons : seasons.slice(0, SEASON_PAGE))
  const hiddenSeasons = $derived(seasons.length - visibleSeasons.length)

  // How many non-special seasons the dialog's "season premiere" date mode
  // cannot date server-side — no premiere, or one still in the future.
  const localToday = todayLocal()
  const seasonsNeedingDate = $derived(
    seasons.filter((s) => s.season_number > 0 && (s.air_date == null || s.air_date > localToday))
      .length,
  )

  // ── Owned copies (D-021) ─────────────────────────────────────────────

  // A show's copies live per-season, and the tiles aggregate coverage over the
  // NON-SPECIAL seasons — Specials never ride a show-level action, exactly as
  // they never gate completion (D-012). Client-side aggregation is fine here:
  // `show.seasons` is complete (uncapped), the seasonsNeedingDate precedent.
  const nonSpecialCopies = $derived(seasons.filter((s) => s.season_number > 0).map((s) => s.copies))

  /** @type {{ src: { id: string, label: string }, have: number, total: number } | null} */
  let copyDialog = $state(null)
  /** @type {string | null} a failed save, shown inside the dialog */
  let copyError = $state(null)
  /**
   * The Jellyfin tile while the library scan owns it (D-033): nothing to
   * confirm, the dialog only says where the answer comes from.
   * @type {{ src: { id: string, label: string }, have: number, total: number } | null}
   */
  let copyInfo = $state(null)

  /** The kinds the server's scan manages, which the tiles render read-only. */
  // $derived.by, not $derived: the callback's scope resets narrowing, so
  // `view` keeps its declared union here instead of its initial value.
  const managedKinds = $derived.by(() =>
    view.state === 'loaded' && view.show.jellyfin_synced ? ['jellyfin'] : [],
  )

  /**
   * Where the Jellyfin server falls short of this show, over the non-special
   * seasons (the same set the tiles aggregate): seasons absent from it, and
   * seasons it holds only part of — fewer episode files than the season's
   * AIRED episodes, the server-derived count the grid's badges already use.
   * A season with no stats (no episode rows yet) cannot be judged and counts
   * as complete. Empty until the scan owns the kind.
   */
  const jellyfinGaps = $derived.by(() => {
    /** @type {{ missing: string[], partial: string[] }} */
    const gaps = { missing: [], partial: [] }
    if (!managedKinds.includes('jellyfin')) return gaps
    for (const s of seasons) {
      if (s.season_number === 0) continue
      if (!s.copies.includes('jellyfin')) {
        gaps.missing.push(s.name)
      } else if (
        s.jellyfin_episodes != null &&
        s.progress != null &&
        s.jellyfin_episodes < s.progress.aired_total
      ) {
        gaps.partial.push(`${s.name} (${s.jellyfin_episodes} of ${s.progress.aired_total})`)
      }
    }
    return gaps
  })
  /** The lit kinds that do not hold everything — the tile turns amber. A
   *  show with nothing on the server is simply unlit, not incomplete. */
  const incompleteKinds = $derived(
    jellyfinGaps.missing.length + jellyfinGaps.partial.length > 0 ? ['jellyfin'] : [],
  )

  /**
   * A copies tile was tapped: open the confirmation. A kind with partial
   * coverage offers BOTH filling in the missing seasons and removing it from
   * all — the choice is the user's, not a guess. A scan-managed kind opens
   * the explanation instead: the server refuses a write on it anyway.
   *
   * @param {{ id: string, label: string }} src
   * @param {{ owned: boolean, have: number, total: number }} s
   */
  function onSelectCopy(src, s) {
    if (view.state !== 'loaded' || tracking) return
    if (managedKinds.includes(src.id)) {
      copyInfo = { src, have: s.have, total: s.total }
      return
    }
    // total 0 is a specials-only show: a fan-out add would honestly write
    // nothing, so there is nothing to offer at show level.
    if (s.total === 0) return
    copyError = null
    copyDialog = { src, have: s.have, total: s.total }
  }

  /** The choices the dialog offers for the tapped kind's current coverage. */
  const copyActions = $derived.by(() => {
    if (copyDialog == null) return []
    const { have, total } = copyDialog
    /** @type {{ id: 'add' | 'remove', label: string, secondary?: boolean }[]} */
    const actions = []
    if (have < total) {
      actions.push({
        id: 'add',
        label: total === 1 ? 'Add to the season' : `Add to all ${total} seasons`,
      })
    }
    if (have > 0) {
      actions.push({
        id: 'remove',
        label: total === 1 ? 'Remove from the season' : 'Remove from all seasons',
        // Outlined only when it stands beside the add above it; a lone remove
        // is the dialog's one command and stays filled.
        secondary: actions.length > 0,
      })
    }
    return actions
  })

  /** The advisories the dialog should carry, joined into its one note slot. */
  const copyNote = $derived.by(() => {
    if (view.state !== 'loaded' || copyDialog == null) return null
    const parts = []
    if (copyDialog.have > 0 && copyDialog.have < copyDialog.total) {
      parts.push(
        `You have ${copyDialog.have} of ${copyDialog.total} seasons on ${copyDialog.src.label}.`,
      )
    }
    if (!view.show.tracked) {
      parts.push('Adding a copy downloads the whole show for offline use.')
    }
    if (seasons.some((s) => s.season_number === 0)) {
      parts.push('Specials are not included — manage them from the Specials season page.')
    }
    return parts.length > 0 ? parts.join(' ') : null
  })

  /**
   * The dialog confirmed one of its actions. Awaited, never optimistic: an
   * add on an untracked show materialises everything, and the per-season
   * coverage afterwards is the server's to state — hence the refetch.
   *
   * @param {string} actionId the dialog's generic action id; this page
   *   offers only 'add' and 'remove'
   */
  async function onConfirmCopy(actionId) {
    if (view.state !== 'loaded' || tracking || copyDialog == null) return
    const show = view.show
    const kind = copyDialog.src.id
    tracking = true
    copyError = null
    try {
      if (actionId === 'add') {
        const res = await addCopy({ targetType: 'show', ...trackTarget(), kind })
        show.id = res.target_id
        show.tracked = true
      } else {
        // Only a show with season copies can lose them, so the id exists.
        await removeCopy('show', /** @type {number} */ (show.id), kind)
      }
      await loadShow(false)
      copyDialog = null
    } catch (err) {
      copyError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  /** @param {number} n */
  const pad = (n) => String(n).padStart(2, '0')

  /**
   * A season of a TRACKED show has a local id and is addressed by it; one of an
   * untracked show has no row at all, so it is addressed by the tmdb pair and
   * the season page serves it live.
   *
   * @param {import('../common/api.js').ShowDetails} show
   * @param {import('../common/api.js').ShowSeason} season
   */
  function seasonHref(show, season) {
    return season.id != null
      ? `/video/season-details?id=${season.id}`
      : `/video/season-details?tmdb_id=${show.tmdb_id}&season=${season.season_number}`
  }

  /** @param {{ season: number, number: number }} e */
  function episodeCode(e) {
    return `S${pad(e.season)}E${pad(e.number)}`
  }

  /** @param {import('../common/api.js').ShowDetails} show */
  function yearRange(show) {
    if (!show.year) return null
    // A returning series has a last_air_date but no end year, so the range is
    // open — and a single-year show should not read "2015–2015".
    if (show.in_production) return `${show.year}–present`
    if (show.end_year && show.end_year !== show.year) return `${show.year}–${show.end_year}`
    return show.year
  }

  /**
   * "3 seasons", "1 season" — the counts sit in a meta line, where a bare
   * number would read as a year.
   *
   * @param {number} n
   * @param {string} noun
   */
  function countLabel(n, noun) {
    return `${n} ${noun}${n === 1 ? '' : 's'}`
  }
</script>

<section class="detail">
  {#if view.state === 'loading'}
    <div class="page-head">
      <h1>Show details</h1>
    </div>
    <p class="status">Loading…</p>
  {:else if view.state === 'missing' || (view.state === 'error' && view.code === 'not_found')}
    <div class="page-head">
      <h1>Show details</h1>
    </div>
    <p class="status">
      This show could not be found.
      <a href="/video/search" use:routeAction>Find it via Search</a>.
    </p>
  {:else if view.state === 'error'}
    <div class="page-head">
      <h1>Show details</h1>
    </div>
    {#if view.code === 'provider_unavailable'}
      <!-- Only an UNTRACKED show can land here: a tracked one has a snapshot
           and is stale-served instead. -->
      <p class="status error">
        This show isn't in your library yet, and TMDB is unreachable right now — shows you have
        marked keep working. Try again later. Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Loading failed ({view.code}).</p>
    {/if}
  {:else}
    {@const show = view.show}
    <!-- TMDB's official gradient, referenced as fill="url(#tmdb-grad)". -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <DetailHero title={show.title} poster={show.poster} posterFull={show.poster_full}>
      {#snippet info()}
        <h1>{show.title}</h1>
        {#if show.created_by.length > 0}
          <p class="directed">Created by {show.created_by.join(', ')}</p>
        {/if}
        <p class="meta-line">
          {#if yearRange(show)}
            <span>{yearRange(show)}</span>
          {/if}
          {#if show.seasons_count > 0}
            <span>{countLabel(show.seasons_count, 'season')}</span>
          {/if}
          {#if show.episodes_count > 0}
            <span>{countLabel(show.episodes_count, 'episode')}</span>
          {/if}
          {#if show.episode_runtime_min != null}
            <!-- A show's episodes vary, so the runtime is approximate. -->
            <span>~{show.episode_runtime_min} min</span>
          {/if}
          {#if show.genres.length > 0}
            <span>{show.genres.join(', ')}</span>
          {/if}
        </p>
      {/snippet}

      {#snippet desc()}
        {#if show.tagline}
          <p class="tagline">{show.tagline}</p>
        {/if}
        {#if show.overview}
          <p class="overview">{show.overview}</p>
        {:else}
          <p class="overview pending">TMDB has no overview for this show.</p>
        {/if}
      {/snippet}
    </DetailHero>

    <div class="detail-body">
      <!-- One column at every width, sections in DOM order. -->
      <div class="content">
        <!-- Outbound links (the one place the app deliberately points away
             from itself) and the facts TMDB carries for a show. -->
        <div class="section s-details">
          <h2 class="section-label">Details</h2>
          <div class="details">
            <div class="ext-links">
              <a
                class="ext-link"
                href="https://www.themoviedb.org/tv/{show.tmdb_id}"
                target="_blank"
                rel="noopener noreferrer"
              >
                <BrandIcon path={TMDB_PATH} size={18} fill="url(#tmdb-grad)" />
                TMDB
              </a>
              {#if show.imdb_id}
                <a
                  class="ext-link"
                  href="https://www.imdb.com/title/{show.imdb_id}/"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <span class="imdb-logo">
                    <BrandIcon path={IMDB_PATH} size={18} />
                  </span>
                  IMDb
                </a>
              {/if}
              {#if show.homepage}
                <a class="ext-link" href={show.homepage} target="_blank" rel="noopener noreferrer">
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
              {#if show.trailer_url}
                <a
                  class="ext-link"
                  href={show.trailer_url}
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

            {#if show.original_language || show.networks.length > 0 || show.show_status || show.first_air_date || show.next_episode || show.popularity != null}
              <div class="facts">
                {#if show.original_language}
                  <div class="fact">
                    <span class="fact-label">Language</span>
                    <span class="fact-value">{languageName(show.original_language)}</span>
                  </div>
                {/if}
                {#if show.networks.length > 0}
                  <div class="fact">
                    <span class="fact-label">Network</span>
                    <span class="fact-value">{show.networks.join(', ')}</span>
                  </div>
                {/if}
                {#if show.show_status}
                  <div class="fact">
                    <span class="fact-label">Status</span>
                    <!-- TMDB's production status, not a watch status: the
                         user's own watching/watched/dropped axis is M2. -->
                    <span class="fact-value">{show.show_status}</span>
                  </div>
                {/if}
                {#if show.first_air_date}
                  <div class="fact">
                    <span class="fact-label">First aired</span>
                    <span class="fact-value">{formatDay(show.first_air_date)}</span>
                  </div>
                {/if}
                {#if show.last_air_date}
                  <div class="fact">
                    <span class="fact-label">{show.in_production ? 'Latest' : 'Last aired'}</span>
                    <span class="fact-value">{formatDay(show.last_air_date)}</span>
                  </div>
                {/if}
                {#if show.next_episode}
                  <div class="fact">
                    <span class="fact-label">Next episode</span>
                    <span class="fact-value">{episodeCode(show.next_episode)}</span>
                    {#if show.next_episode.air_date}
                      <span class="fact-note">{formatDay(show.next_episode.air_date)}</span>
                    {/if}
                  </div>
                {/if}
                {#if show.popularity != null}
                  <div class="fact">
                    <span class="fact-label">Popularity</span>
                    <span class="fact-value">{show.popularity.toFixed(1)}</span>
                    <!-- Popularity is TMDB's trending metric — only as current
                         as the fetch, so date it. -->
                    <span class="fact-note">as of {formatDay(localDayOf(show.fetched_at))}</span>
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
                    {show.rating != null ? show.rating.toFixed(1) : '—'}
                  </span>
                  {#if show.votes != null}
                    <span class="score-votes">{show.votes.toLocaleString()} votes</span>
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

            <!-- A third card, a peer of the two above: your episode ratings
                 across every season folded into one mean — what the season
                 posters' hearts add up to. Its own card rather than a line
                 under the bar, because the bar IS your rating and this is a
                 different figure. Server-computed, weighted per episode (a
                 mean of the seasons' means would let a season with one rated
                 episode count as much as one with twelve). Read off the loaded
                 show, so the page's re-reads keep it current; rating an
                 episode happens on other pages. On wide screens the row wraps
                 (detail-page.css), so this sits beside "Your rating" when it
                 fits and drops to its own line when it does not. -->
            <div class="ratings-block avg-block">
              <h2 class="section-label">Your average</h2>
              <EpisodeAverage
                value={show.user_episode_rating}
                rated={show.user_rated_episodes}
                total={show.episodes_count}
                note="Average of your episode ratings across every season."
                emptyNote="Rate episodes and their average appears here."
              />
            </div>
          </div>
        </div>

        <WatchNowSection
          seasonCopies={nonSpecialCopies}
          {managedKinds}
          {incompleteKinds}
          streaming={show.streaming}
          subject="show"
          disabled={tracking}
          onselect={onSelectCopy}
        />

        <!-- Progress first, then the log it summarises: this page owes the
             one-line answer to "where am I", and the timeline below answers
             "what did I watch, when". Both figures here are derived from the
             watch log, never stored (D-010), which is why removing a watch
             changes them. -->
        {#if status || progress}
          <div class="section s-progress">
            <h2 class="section-label">Your progress</h2>
            <div class="card quiet">
              <p class="progress-line">
                {#if status}
                  <span class="status-pill {status}">{STATUS_LABELS[status] ?? status}</span>
                {/if}
                {#if progress}
                  <span class="progress-count">
                    {progress.watched} / {progress.aired_total} aired episodes
                  </span>
                  {#if progress.total > progress.aired_total}
                    <!-- Aired is the denominator completion turns on, so the
                         full count is secondary rather than the headline. -->
                    <span class="progress-total">{progress.total} in total</span>
                  {/if}
                {/if}
                {#if status}
                  <!-- A state row exists if and only if the watch log holds an
                       episode of this show, or the user already dropped it —
                       status.go's ensure creates the row on any episode watch
                       (specials included) and its collapse removes it when the
                       last one goes. So a non-null status IS "at least one
                       episode in the history", which is the condition this
                       button needs and the one progress.watched cannot state:
                       that counts aired non-specials only, so watching just a
                       special or an unaired episode leaves it at 0. -->
                  <button
                    class="drop-btn"
                    class:resume={status === 'dropped'}
                    onclick={onToggleDrop}
                    disabled={tracking}
                  >
                    {status === 'dropped' ? 'Resume show' : 'Drop show'}
                  </button>
                {/if}
              </p>
            </div>
          </div>
        {/if}

        <!-- Scoped by show: every episode watch across every season, so each
             card carries the full SxxExx code rather than just the number. An
             untracked show has no episodes and so no request. -->
        <HistoryTimeline
          showId={show.id}
          reloadToken={historyToken}
          emptyNote="You have not watched any episode of this show yet."
          ondeleted={onWatchDeleted}
        />

        <div class="section s-actions">
          <div class="actions">
            <button
              class="watch-cta"
              class:done={watched}
              onclick={onMarkWatched}
              disabled={tracking}
              aria-pressed={watched}
            >
              {#if tracking}
                Saving…
              {:else}
                <!-- Never a dead end: a finished show can be logged again, the
                     movie and episode pages' precedent. -->
                {watched ? 'Log another watch' : 'Mark As Watched'}
              {/if}
            </button>
            <!-- The other axis, never a status (D-011). Filled bookmark when
                 listed, bookmark-with-a-plus when not. -->
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
          {:else if !show.tracked}
            <!-- Any of the three buttons pulls the show local, so say what
                 that costs before the user commits to it. -->
            <p class="demo-hint">
              Marking, watchlisting or favoriting downloads the whole show for offline use; marking
              also marks every aired episode.
            </p>
          {/if}
        </div>

        <div class="section s-cast">
          <h2 class="section-label">Cast</h2>
          <CastRow
            cast={show.cast}
            title={show.title}
            emptyNote="TMDB lists no cast for this show."
          />
        </div>

        <!-- Seasons are the one section a show has and a movie does not: a
             grid rather than a rail, because seasons are the thing you come
             here to scan. Each card links to the season page — by local id
             when a snapshot row exists, else by show tmdb_id + number. -->
        <div class="section s-seasons">
          <h2 class="section-label">Seasons</h2>
          {#if seasons.length > 0}
            <div class="season-grid">
              {#each visibleSeasons as season (season.season_number)}
                <a
                  class="season-card"
                  href={seasonHref(show, season)}
                  use:routeAction
                  style="--poster-hue: {(hueFor(show.title) + season.season_number * 28) % 360}"
                >
                  <div class="season-poster">
                    {#if season.poster}
                      <img
                        src={assetUrl(season.poster)}
                        srcset={posterSrcset(season.poster, season.poster_full)}
                        sizes="(min-width: 1100px) 160px, 140px"
                        alt=""
                        loading="lazy"
                      />
                    {:else}
                      <span class="season-initials">{initialsFor(season.name)}</span>
                    {/if}
                    {#if season.user_rating != null}
                      <!-- The average of this user's episode ratings for the
                           season (a season is not rateable, D-020), in the
                           history card's top-right "your rating" pill so the
                           user's own number sits in the same corner, in the
                           same red, on every poster in the app. One decimal,
                           unlike the card's integer: this is a mean. -->
                      <span
                        class="user-rating"
                        title="Your rating — the average of your episode ratings"
                      >
                        {season.user_rating.toFixed(1)}
                      </span>
                    {/if}
                    {#if season.rating != null}
                      <!-- TMDB's season score, badged like the search cards
                           do it. -->
                      <span class="season-rating">
                        <span class="heart">♥</span>
                        {season.rating.toFixed(1)}
                      </span>
                    {/if}
                    {#if season.progress != null && season.progress.watched > 0}
                      <!-- Derived per-season progress (D-010), watched / aired as
                           the show's own line reads. Hidden until something is
                           watched, so an unstarted season stays a plain poster;
                           accent once the aired run is complete — "finished",
                           the watchlist card's ✓. -->
                      <span
                        class="season-progress"
                        class:done={season.progress.watched === season.progress.aired_total}
                        title="{season.progress.watched} of {season.progress
                          .aired_total} aired episodes watched"
                      >
                        {season.progress.watched}/{season.progress.aired_total}
                      </span>
                    {/if}
                  </div>
                  <p class="season-name" title={season.name}>{season.name}</p>
                  <p class="season-meta">
                    {#if season.episode_count > 0}
                      <span>{countLabel(season.episode_count, 'ep')}</span>
                    {/if}
                    {#if season.year}
                      <span>{season.year}</span>
                    {/if}
                  </p>
                </a>
              {/each}
              {#if hiddenSeasons > 0}
                <!-- The expander is a grid cell of its own, poster-shaped like
                     the cards around it — "more posters behind this", right
                     where the grid ends. Surface-toned rather than hue-tinted
                     so it reads as a control, not another season. -->
                <button
                  class="season-card season-more"
                  onclick={() => (allSeasons = true)}
                  aria-label="Show {hiddenSeasons} more {hiddenSeasons === 1
                    ? 'season'
                    : 'seasons'}"
                >
                  <span class="season-poster">
                    <span class="more-count">+{hiddenSeasons}</span>
                    <span class="more-label">more {hiddenSeasons === 1 ? 'season' : 'seasons'}</span
                    >
                  </span>
                </button>
              {/if}
            </div>
          {:else}
            <p class="row-note pending">TMDB lists no seasons for this show.</p>
          {/if}
        </div>

        <div class="section s-media">
          <h2 class="section-label">Media</h2>
          <MediaGallery
            title={show.title}
            videos={show.videos}
            backdrops={show.backdrops}
            posters={show.posters}
            emptyNote="TMDB lists no media for this show."
          />
        </div>

        <!-- TMDB's recommendations, out of the same snapshot as everything
             above (D-024): a tracked show keeps its rail with TMDB down. Every
             card is a title the library has no row for, reached by tmdb_id. -->
        <div class="section s-similar">
          <h2 class="section-label">Similar shows</h2>
          <SimilarRow items={show.similar} kind="show" emptyNote="TMDB lists no similar shows." />
        </div>
      </div>
    </div>

    {#if logOpen}
      <WatchLogDialog
        title={show.title}
        busy={tracking}
        error={logError}
        note={!show.tracked ? 'Marking downloads the whole show for offline use.' : null}
        bulk={{
          watched: progress?.watched ?? 0,
          aired: progress?.aired_total ?? 0,
          seasonsNeedingDate,
          onconfirm: onBulkLog,
        }}
        oncancel={() => (logOpen = false)}
      />
    {/if}

    {#if dropOpen}
      <!-- One action, so it reads as a confirmation rather than a fork. The
           note is the whole reason this is a dialog and not a bare toggle:
           neither consequence is visible from the button. -->
      <ChoiceDialog
        heading={status === 'dropped' ? 'Resume this show?' : 'Drop this show?'}
        subject={show.title}
        actions={[
          status === 'dropped'
            ? { id: 'resume', label: 'Resume show' }
            : { id: 'drop', label: 'Drop show' },
        ]}
        note={status === 'dropped'
          ? 'Its status goes back to whatever your watch history implies.'
          : 'It stops appearing in Up Next and stays dropped as new episodes air. Your watch history is kept.'}
        busy={tracking}
        error={dropError}
        onconfirm={onConfirmDrop}
        oncancel={() => (dropOpen = false)}
      />
    {/if}

    {#if copyDialog}
      <ChoiceDialog
        heading={copyDialog.have === 0
          ? `Add ${copyDialog.src.label} copy?`
          : copyDialog.have === copyDialog.total
            ? `Remove ${copyDialog.src.label} copy?`
            : `${copyDialog.src.label} copies`}
        subject={show.title}
        actions={copyActions}
        note={copyNote}
        busy={tracking}
        error={copyError}
        onconfirm={onConfirmCopy}
        oncancel={() => (copyDialog = null)}
      />
    {/if}

    {#if copyInfo}
      <!-- The scan's tile (D-033): the answer is the server's, per season, so
           the one action is the way to the scan's page. -->
      <ChoiceDialog
        heading={copyInfo.have === 0
          ? `Not on ${copyInfo.src.label}`
          : copyInfo.have === copyInfo.total
            ? `On ${copyInfo.src.label}`
            : `${copyInfo.have} of ${copyInfo.total} seasons on ${copyInfo.src.label}`}
        subject={show.title}
        note={[
          "This tile mirrors your Jellyfin server's library, season by season, and turns amber while any season is absent or missing aired episodes.",
          jellyfinGaps.missing.length > 0
            ? `Not on the server: ${jellyfinGaps.missing.join(', ')}.`
            : '',
          jellyfinGaps.partial.length > 0
            ? `Only partly there: ${jellyfinGaps.partial.join(', ')} aired episodes.`
            : '',
          "It updates on the next scan and can't be set by hand.",
        ]
          .filter(Boolean)
          .join(' ')}
        actions={[{ id: 'jellyfin', label: 'Jellyfin scan settings', href: '/settings/jellyfin' }]}
        cancelLabel="Close"
        oncancel={() => (copyInfo = null)}
      />
    {/if}
  {/if}
</section>

<style>
  /* This page owns its seasons grid and its progress line. Everything else is
     shared with the movie details page — see ./detail-page.css and
     ./components/. */

  /* ── Progress line ─────────────────────────────────────────────────── */

  .progress-line {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.6rem;
    margin: 0;
    font-size: 0.9rem;
  }

  /* The three statuses read as three different things, so they are coloured
     rather than left as one neutral chip: in progress green, done accent,
     abandoned red. Each is its colour's tint behind that colour's text — the
     same anatomy, so the three read as one system. */
  .status-pill {
    padding: 0.2rem 0.6rem;
    border-radius: 999px;
    font-size: 0.75rem;
    font-weight: 600;
  }

  .status-pill.watching {
    background: rgb(55 150 85 / 0.8);
    color: #fff;
  }

  .status-pill.watched {
    background: var(--accent-tint-strong);
    color: var(--accent);
  }

  /* Red, not grey: abandoned is a decision the user made, and grey-on-dim
     read as "disabled" — as if the app, not the user, had switched it off. */
  .status-pill.dropped {
    background: var(--err-tint-strong);
    color: var(--err);
  }

  .progress-count {
    font-weight: 600;
  }

  /* Secondary on purpose: aired is the denominator completion turns on, so a
     full count next to a status of Watched must not read as a contradiction. */
  .progress-total {
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  /* The status axis's own control, sitting with the label it changes. Quiet on
     purpose — dropping is a rare, deliberate correction and must not compete
     with the page's watch CTA. margin-left:auto pushes it to the far end of the
     line, and the line's own flex-wrap drops it under the counts when there is
     no room. */
  .drop-btn {
    margin-left: auto;
    padding: 0.3rem 0.7rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.78rem;
    cursor: pointer;
    transition:
      border-color 0.15s ease,
      color 0.15s ease;
  }

  .drop-btn:hover:enabled {
    border-color: var(--accent);
    color: var(--text);
  }

  .drop-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* Disabled here means a write is in flight, matching the action buttons. */
  .drop-btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  /* "Resume show" wears the dropped pill's colour, so the state and the way out
     of it read as one thing. After the rules above on purpose: the base hover
     turns the border --accent, which this has to take back. */
  .drop-btn.resume {
    border-color: var(--err);
    color: var(--err);
  }

  .drop-btn.resume:hover:enabled {
    background: var(--err-tint);
    border-color: var(--err);
    color: var(--text);
  }

  @media (hover: none) {
    .drop-btn {
      min-height: 2.75rem;
    }
  }

  /* ── Seasons grid ──────────────────────────────────────────────────── */

  /* auto-fill rather than a fixed count: the grid is the widest thing on the
     page, and a show with three seasons should not stretch them to fill it —
     minmax caps the cell, so they stay poster-sized and left-aligned. */
  .season-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 1.1rem 0.85rem;
  }

  /* The cards are anchors now: keep the text styling theirs, and put the
     clickable affordance on the poster's border, matching how the search
     cards signal it. */
  .season-card {
    display: flex;
    flex-direction: column;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }

  .season-card:hover .season-poster,
  .season-card:focus-visible .season-poster {
    border-color: var(--accent);
  }

  .season-card:hover .season-name {
    color: var(--accent);
  }

  .season-card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-md);
  }

  .season-poster {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    transition: border-color 0.15s ease;
    overflow: hidden;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 18% 26%),
      hsl(var(--poster-hue) 22% 13%)
    );
  }

  .season-poster img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .season-initials {
    font-size: 1.4rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    color: rgb(255 255 255 / 0.32);
  }

  /* The same corner badges the history card wears (./components/
     HistoryCard.svelte), so the grids read as one system: progress top-left,
     your rating top-right, TMDB's score bottom-right. All three share one
     geometry, scaled down for the narrower season cells; each rounds only its
     inner corner, the poster's overflow clips the outer. */
  .season-rating,
  .season-progress,
  .user-rating {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.2rem 0.45rem;
    background: rgb(0 0 0 / 0.35);
    backdrop-filter: blur(4px);
    color: #fff;
    font-size: 0.75rem;
    font-weight: 600;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.85);
  }

  .season-rating {
    right: 0;
    bottom: 0;
    border-radius: 0.4rem 0 0 0;
  }

  .season-progress {
    left: 0;
    top: 0;
    border-radius: 0 0 0.4rem 0;
    font-variant-numeric: tabular-nums;
  }

  /* Complete: solid accent, the one badge state that means "finished". */
  .season-progress.done {
    background: var(--accent);
    text-shadow: none;
  }

  /* The history card's red "your rating" pill, at the progress badge's size
     rather than the card's own: the two top corners must match, and a bigger
     chip crowds these small posters. Solid red, no shadow — a badge, not a
     translucent overlay. */
  .user-rating {
    top: 0;
    right: 0;
    border-radius: 0 0 0 0.4rem;
    background: #e8435a;
    text-shadow: none;
  }

  .season-rating .heart {
    color: #f2555a;
    font-size: 0.85rem;
    line-height: 1;
  }

  .season-name {
    margin: 0.45rem 0 0;
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .season-meta {
    margin: 0.1rem 0 0;
    font-size: 0.72rem;
    color: var(--text-dim);
  }

  /* Same separator the hero's meta line uses, so the two read as one system. */
  .season-meta span:not(:first-child)::before {
    content: '·';
    margin: 0 0.35rem;
  }

  /* The expander cell: a bare button so the poster face carries all the
     chrome — sharing .season-card/.season-poster keeps the hover accent and
     focus outline identical to the cards around it. */
  .season-more {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    cursor: pointer;
  }

  /* Surface-toned, not hue-tinted: a control, not another season. The column
     override stacks the count over its label (the cards' rule centers a
     single row). */
  .season-more .season-poster {
    flex-direction: column;
    gap: 0.3rem;
    background: var(--surface);
  }

  .more-count {
    font-size: 1.4rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    color: var(--text);
  }

  .more-label {
    font-size: 0.72rem;
    color: var(--text-dim);
  }

  @media (min-width: 1100px) {
    .season-grid {
      grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    }
  }

  @media (max-width: 599px) {
    .season-grid {
      grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
    }
  }
</style>
