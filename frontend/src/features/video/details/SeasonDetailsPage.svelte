<script>
  // Season details. Addressed by local ?id= once the show is tracked, else by
  // ?tmdb_id= (the SHOW's) + ?season= together. Exactly the movie and show
  // views' shape (D-015): an untracked show's season is fetched live and stored
  // nowhere, so it carries no id and can 503 on an outage; a tracked one is
  // served from its snapshot, refreshed past 48h, and stale-served when TMDB is
  // down — a season with a row never fails.
  //
  // The hero, rating bar and page chrome are shared with the other detail
  // pages (./components/, ./detail-page.css), and so is the cast rail — the
  // season's own aggregate credits, through the show page's component. What is
  // here is what is genuinely season-shaped: the episode list, each row a link
  // into the episode page.
  //
  // The grid itself writes nothing: an episode's still only DISPLAYS whether
  // it is watched (and how the user rated it), wearing the history feed's
  // badges. Marking from this page is the season-level "Mark As Watched"
  // below the grid, which opens WatchLogDialog in its bulk mode: remaining
  // episodes or a full rewatch, dated now, at a fixed instant, or from the
  // episode/season release dates — the show page's dialog, scoped to one
  // season. A single episode is marked from its own page, dated.
  //
  // A SEASON itself is not rateable — only movies, shows and episodes are
  // (D-020) — so the "Your rating" slot holds no control: it shows the average
  // of this user's episode ratings, the season-level read D-020 says episode
  // ratings already provide.

  import { route as routeAction } from '@mateothegreat/svelte5-router'
  import { assetUrl, backdropSrcset } from '../../../lib/images.js'
  import { hueFor } from '../common/posterPlaceholder.js'
  import { TMDB_PATH } from '../../../lib/brandMarks.js'
  import { formatDay, localDayOf, todayLocal } from '../common/format.js'
  import { getSeasonDetails, markSeasonWatched, addCopy, removeCopy } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'
  import EpisodeAverage from './EpisodeAverage.svelte'
  import BrandIcon from '../common/components/BrandIcon.svelte'
  import CastRow from './CastRow.svelte'
  import ChoiceDialog from '../common/components/ChoiceDialog.svelte'
  import DetailHero from '../common/components/DetailHero.svelte'
  import DroppedNotice from './DroppedNotice.svelte'
  import HistoryTimeline from './HistoryTimeline.svelte'
  import WatchLogDialog from '../common/components/WatchLogDialog.svelte'
  import WatchNowSection from './WatchNowSection.svelte'
  import '../common/detail-page.css'

  /** @type {{ route: any }} */
  let { route } = $props()

  // The params travel in the query string, read once per mount — the router
  // remounts the component on navigation, so there is nothing to react to.
  // svelte-ignore state_referenced_locally
  const qs =
    route?.result?.querystring?.params ??
    Object.fromEntries(new URLSearchParams(window.location.search))
  const localId = qs.id
  const tmdbId = qs.tmdb_id
  const seasonNumber = qs.season

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'missing' }
   *   | { state: 'error', code: string }
   *   | { state: 'loaded', season: import('../common/api.js').SeasonDetails }}
   */
  let view = $state({ state: 'loading' })

  /**
   * Load the season. Also the re-read after a bulk mark: the episodes'
   * `watched` flags and any minted ids are the server's to state, never
   * recomputed client-side.
   *
   * @param {boolean} [initial] false when refreshing an already-rendered page,
   *   which must not fall back to an error screen over a transient failure
   * @returns {Promise<void>} settles when the view is up to date (or kept)
   */
  function loadSeason(initial = true) {
    // Prefer a local id, which exists only once the show is tracked; otherwise
    // the tmdb pair, which addresses the season either way. A refresh after the
    // bulk mark tracks the show goes out on the pair and comes back WITH an id,
    // which is how the page adopts one mid-session.
    const loadedId = view.state === 'loaded' ? view.season.id : null
    const addressed =
      loadedId != null
        ? { id: loadedId }
        : localId != null
          ? { id: localId }
          : { tmdbId, season: seasonNumber }
    return getSeasonDetails(addressed)
      .then((season) => {
        view = { state: 'loaded', season }
      })
      .catch((err) => {
        // A failed refresh leaves the page as it was: the ticks are one action
        // stale, which beats replacing a working page with an error.
        if (initial) {
          view = { state: 'error', code: err instanceof ApiError ? err.code : 'unreachable' }
        }
      })
  }

  if (localId == null && (tmdbId == null || seasonNumber == null)) {
    view = { state: 'missing' }
  } else {
    loadSeason()
  }

  // A season is deliberately NOT rateable (D-020): a season is a packaging
  // unit, and the show and the episode are the two things a person actually
  // forms an opinion about. "Season 4 was the good one" is a statement about
  // its episodes, so the slot shows their ratings folded into one average —
  // a derived read, never a value the server would refuse to store.
  const ratedEpisodes = $derived.by(() => {
    if (view.state !== 'loaded')
      return /** @type {import('../common/api.js').SeasonEpisode[]} */ ([])
    return view.season.episodes.filter((e) => e.user_rating != null)
  })
  const avgRating = $derived(
    ratedEpisodes.length > 0
      ? ratedEpisodes.reduce((sum, e) => sum + (e.user_rating ?? 0), 0) / ratedEpisodes.length
      : null,
  )

  // ── The season-level bulk mark ────────────────────────────────────────

  // Whether the bulk dialog is up, a write in flight, and a failed save shown
  // inside the dialog — the other detail pages' logOpen/logError pattern.
  let logOpen = $state(false)
  let tracking = $state(false)
  /** @type {string | null} */
  let logError = $state(null)

  // Display-only derivations for the button label and the dialog's counts.
  const localToday = todayLocal()
  const episodes = $derived.by(() => (view.state === 'loaded' ? view.season.episodes : []))
  const airedEps = $derived(episodes.filter((e) => e.air_date != null && e.air_date <= localToday))
  const watchedAired = $derived(airedEps.filter((e) => e.watched).length)
  const seasonWatched = $derived(airedEps.length > 0 && watchedAired === airedEps.length)
  // Whether season_air needs the fallback date: 0 or 1 here — one season.
  const seasonNeedsDate = $derived.by(() => {
    if (view.state !== 'loaded') return 0
    const premiere = view.season.air_date
    return premiere == null || premiere > localToday ? 1 : 0
  })

  /** @param {unknown} err */
  function bulkErrorMessage(err) {
    if (err instanceof ApiError && err.code === 'provider_unavailable') {
      return 'TMDB is unreachable right now — try again in a moment. Your library is unaffected.'
    }
    return 'Saving failed — try again.'
  }

  function onMarkSeason() {
    if (view.state !== 'loaded' || tracking) return
    logError = null
    logOpen = true
  }

  /**
   * The dialog confirmed. Awaited, never optimistic: the first mark
   * materialises the whole show, which takes seconds and can genuinely fail —
   * and the refetch afterwards is what flips the grid's ticks and adopts any
   * minted episode ids, because those are the server's to state.
   *
   * @param {{ rewatch: boolean, mode: 'now'|'fixed'|'episode_air'|'season_air',
   *   watchedAt: string | null }} choice
   */
  async function onBulkLog({ rewatch, mode, watchedAt }) {
    if (view.state !== 'loaded' || tracking) return
    const season = view.season
    tracking = true
    logError = null
    try {
      await markSeasonWatched({
        id: season.id,
        tmdbId: season.show_tmdb_id,
        season: season.season_number,
        rewatch,
        mode,
        watchedAt,
      })
      await loadSeason(false)
      historyToken++
      logOpen = false
    } catch (err) {
      logError = bulkErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // ── Owned copies (D-021) ─────────────────────────────────────────────

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
    view.state === 'loaded' && view.season.jellyfin_synced ? ['jellyfin'] : [],
  )

  // The scan's episode-file count for this season, or null when it is not
  // there. Fewer files than AIRED episodes means the server holds some of the
  // season, not all of it — an unaired episode cannot be there yet, so the
  // aired list is the honest denominator, and it already exists above.
  const jellyfinEpisodes = $derived.by(() =>
    view.state === 'loaded' ? view.season.jellyfin_episodes : null,
  )
  const jellyfinIncomplete = $derived(
    jellyfinEpisodes != null && jellyfinEpisodes < airedEps.length,
  )
  /** The lit kinds that do not hold everything — the tile turns amber. */
  const incompleteKinds = $derived(jellyfinIncomplete ? ['jellyfin'] : [])

  /**
   * A copies tile was tapped: open the confirmation. This page is where a
   * SINGLE season's copies are managed — Specials included, which the show
   * page's bulk action deliberately skips. A scan-managed kind opens the
   * explanation instead: the server refuses a write on it anyway.
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

  /**
   * The dialog confirmed. Awaited, never optimistic: the first copy on an
   * untracked show materialises the whole show — and the refetch afterwards is
   * what adopts the minted id and the fresh copies, because those are the
   * server's to state.
   */
  async function onConfirmCopy() {
    if (view.state !== 'loaded' || tracking || copyDialog == null) return
    const season = view.season
    const { src, owned } = copyDialog
    tracking = true
    copyError = null
    try {
      if (owned) {
        // Only a season with a catalog row can carry a copy, so the id exists.
        await removeCopy('season', /** @type {number} */ (season.id), src.id)
      } else {
        await addCopy(
          season.id != null
            ? { targetType: 'season', id: season.id, kind: src.id }
            : {
                targetType: 'season',
                tmdbId: season.show_tmdb_id,
                season: season.season_number,
                kind: src.id,
              },
        )
      }
      await loadSeason(false)
      copyDialog = null
    } catch (err) {
      copyError = bulkErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // Bumped after a write that adds watch rows, to make HistoryTimeline
  // refetch — the feed spans every episode here, so the bulk mark belongs in it.
  let historyToken = $state(0)

  /**
   * The timeline removed a watch. The feed covers the whole season, so the
   * count that matters is the one for the episode that lost a row — when it
   * hits zero the server has un-watched that episode, and the grid's ✓ badge
   * above would otherwise contradict the feed below it.
   *
   * @param {import('../common/api.js').HistoryEntry} deleted
   * @param {number | null} remaining watches of THAT episode still loaded, or
   *   null when a later page might hold more — in which case it stays watched
   */
  function onWatchDeleted(deleted, remaining) {
    if (view.state !== 'loaded' || remaining !== 0) return
    const number = deleted.episode?.number
    const ep = view.season.episodes.find((e) => e.episode_number === number)
    if (ep) ep.watched = false
  }

  // The same vocabulary the up-next cards use — the API's `marker` values are
  // shared between the two so the badges read as one system.
  const MARKER_LABELS = {
    season_premiere: 'Season Premiere',
    mid_season_finale: 'Mid-Season Finale',
    season_finale: 'Season Finale',
  }

  /** @param {number} n */
  const pad = (n) => String(n).padStart(2, '0')

  /** @param {import('../common/api.js').SeasonEpisode} ep */
  const epTitle = (ep) => ep.title ?? `Episode ${ep.episode_number}`

  // The local form when the show is tracked and the season has a row, else the
  // tmdb pair — the same two rungs the show page's season links use, for the
  // same reason: only tracking creates a season row (D-015).
  /**
   * @param {import('../common/api.js').SeasonDetails} season
   * @param {import('../common/api.js').SeasonEpisode} ep
   */
  const episodeHref = (season, ep) =>
    season.id != null
      ? `/video/episode-details?season_id=${season.id}&episode=${ep.episode_number}`
      : `/video/episode-details?tmdb_id=${season.show_tmdb_id}&season=${season.season_number}` +
        `&episode=${ep.episode_number}`

  /**
   * "13 episodes", "1 episode" — in a meta line a bare number would read as a
   * year.
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
      <h1>Season details</h1>
    </div>
    <p class="status">Loading…</p>
  {:else if view.state === 'missing' || (view.state === 'error' && view.code === 'not_found')}
    <div class="page-head">
      <h1>Season details</h1>
    </div>
    <p class="status">
      This season could not be found.
      <a href="/video/search" use:routeAction>Find its show via Search</a>.
    </p>
  {:else if view.state === 'error'}
    <div class="page-head">
      <h1>Season details</h1>
    </div>
    {#if view.code === 'provider_unavailable'}
      <!-- Only a season with no snapshot row can land here: one that has been
           viewed before is stale-served instead. -->
      <p class="status error">
        This season hasn't been viewed before, and TMDB is unreachable right now — previously viewed
        seasons keep working. Try again later. Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Loading failed ({view.code}).</p>
    {/if}
  {:else}
    {@const season = view.season}
    <!-- TMDB's official gradient, referenced as fill="url(#tmdb-grad)". -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <DetailHero title={season.name} poster={season.poster} posterFull={season.poster_full}>
      {#snippet info()}
        <!-- The season belongs to its show; the way back up is part of the
             page's identity, so it sits above the title. -->
        <p class="show-link">
          <a href="/video/show-details?tmdb_id={season.show_tmdb_id}" use:routeAction>
            ‹ {season.show_title}
          </a>
        </p>
        <h1>{season.name}</h1>
        <p class="meta-line">
          {#if season.year}
            <span>{season.year}</span>
          {/if}
          {#if season.episode_count > 0}
            <span>{countLabel(season.episode_count, 'episode')}</span>
          {/if}
          {#if season.air_date}
            <span>Premiered {formatDay(season.air_date)}</span>
          {/if}
        </p>
      {/snippet}

      {#snippet desc()}
        {#if season.overview}
          <p class="overview">{season.overview}</p>
        {:else}
          <p class="overview pending">TMDB has no overview for this season.</p>
        {/if}
      {/snippet}
    </DetailHero>

    <div class="detail-body">
      <div class="content">
        <!-- The owning show is dropped (D-010). Everything below still works —
             the notice says so — but it goes first: it changes how the rest of
             the page should be read. The link is the way to un-drop it, which
             is a show-page action. -->
        {#if season.show_dropped}
          <DroppedNotice
            showTitle={season.show_title}
            href="/video/show-details?tmdb_id={season.show_tmdb_id}"
          />
        {/if}

        <!-- External score and the user's own rating, same anatomy as the
             other detail pages' ratings row. -->
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
                    {season.rating != null ? season.rating.toFixed(1) : '—'}
                  </span>
                  <span class="score-votes pending">season score</span>
                </div>
              </div>
            </div>

            <!-- Not a control (D-020): the slot holds the average of this
                 user's episode ratings, the shared read the show page also
                 carries under its own bar. -->
            <div class="ratings-block rate-block">
              <h2 class="section-label">Your rating</h2>
              <EpisodeAverage
                value={avgRating}
                rated={ratedEpisodes.length}
                total={season.episodes.length}
                note="Average of your episode ratings — seasons aren't rated directly."
                emptyNote="Rate episodes and their average appears here — seasons aren't rated directly."
              />
            </div>
          </div>
        </div>

        <!-- This season's owned copies — the per-season half of what the show
             page's tiles aggregate, and the only place a Specials copy is
             managed. The streaming half beside them is the OWNING SHOW's
             (D-022): TMDB publishes providers per show, never per season, so
             the server hands down the show's listing and it is empty — hiding
             the sub-group — while the show is untracked. -->
        <WatchNowSection
          copies={season.copies}
          {managedKinds}
          {incompleteKinds}
          streaming={season.streaming}
          subject="show"
          disabled={tracking}
          onselect={onSelectCopy}
        />

        <!-- The episode grid is what a season page is for: every episode TMDB
             lists, aired or not, as still-image cards wearing the history
             card's badges — the ✓ when watched, this user's rating, the ♥
             score — plus the up-next cards' premiere/finale marker. Nothing on
             a still is a control: the whole card is one link to the episode
             page. The overview is deliberately not on a card; it still travels
             in the API response for the episode view. -->
        <div class="section s-episodes">
          <h2 class="section-label">Episodes</h2>
          {#if season.episodes.length > 0}
            <div class="episode-grid">
              {#each season.episodes as ep (ep.episode_number)}
                <article class="ep-card">
                  <a class="ep-link" href={episodeHref(season, ep)} use:routeAction>
                    <div
                      class="ep-still"
                      style="--poster-hue: {(hueFor(season.show_title) + ep.episode_number * 28) %
                        360}"
                    >
                      {#if ep.still}
                        <img
                          src={assetUrl(ep.still)}
                          srcset={backdropSrcset(ep.still, ep.still_full)}
                          sizes="(max-width: 599px) 50vw, 300px"
                          alt=""
                          loading="lazy"
                        />
                      {:else}
                        <span class="ep-still-code">E{pad(ep.episode_number)}</span>
                      {/if}
                      <!-- Read-only, unlike the history card's always-on mark:
                           a season lists unwatched episodes too. -->
                      {#if ep.watched}
                        <span class="check" aria-label="Watched">✓</span>
                      {/if}
                      <!-- Only when this user rated the episode; unrated shows
                           no badge. -->
                      {#if ep.user_rating != null}
                        <span class="user-rating">{ep.user_rating}</span>
                      {/if}
                      {#if ep.rating != null}
                        <span class="rating">
                          <span class="heart">♥</span>
                          {ep.rating.toFixed(1)}
                        </span>
                      {/if}
                      {#if ep.marker}
                        <span class="marker {ep.marker}">{MARKER_LABELS[ep.marker]}</span>
                      {/if}
                    </div>
                    <div class="ep-meta">
                      <h3 class="ep-title" title={`E${pad(ep.episode_number)} ${epTitle(ep)}`}>
                        <span class="ep-code">E{pad(ep.episode_number)}</span>
                        {epTitle(ep)}
                      </h3>
                      {#if ep.air_date || ep.runtime_min != null}
                        <p class="ep-facts">
                          {#if ep.air_date}
                            <span>{formatDay(ep.air_date)}</span>
                          {/if}
                          {#if ep.runtime_min != null}
                            <span>{ep.runtime_min} min</span>
                          {/if}
                        </p>
                      {/if}
                    </div>
                  </a>
                </article>
              {/each}
            </div>
          {:else}
            <p class="row-note pending">TMDB lists no episodes for this season.</p>
          {/if}
        </div>

        <!-- The season-level bulk mark, hidden for Specials (they never gate
             completion, and the server refuses them) and for a season with
             nothing aired. Same shared button chrome as the other pages'. -->
        {#if season.season_number > 0 && airedEps.length > 0}
          <div class="section s-actions">
            <div class="actions">
              <button
                class="watch-cta"
                class:done={seasonWatched}
                onclick={onMarkSeason}
                disabled={tracking}
                aria-pressed={seasonWatched}
              >
                {#if tracking}
                  Saving…
                {:else}
                  {seasonWatched ? 'Log another watch' : 'Mark As Watched'}
                {/if}
              </button>
            </div>
          </div>
        {/if}

        <!-- This season's cast — TMDB's aggregate credits for the season alone,
             so the episode count under each part is within the season, not the
             run. The same rail the show page renders; the photos are the same
             files, served from the show's asset tree once it is tracked. -->
        <div class="section s-cast">
          <h2 class="section-label">Cast</h2>
          <CastRow
            cast={season.cast}
            title={season.show_title}
            emptyNote="TMDB lists no cast for this season."
          />
        </div>

        <!-- Scoped by season rather than by target: this is the one page whose
             feed spans many episodes, so each card names its own. -->
        <HistoryTimeline
          seasonId={season.id}
          reloadToken={historyToken}
          emptyNote="You have not watched any episode of this season yet."
          ondeleted={onWatchDeleted}
        />

        <!-- The snapshot is only as current as its fetch, so date it — the
             other detail pages do the same. -->
        <div class="section s-provenance">
          <p class="row-note pending">
            Snapshot as of {formatDay(localDayOf(season.fetched_at))}.
          </p>
        </div>
      </div>
    </div>

    {#if logOpen}
      <WatchLogDialog
        title="{season.show_title} — {season.name}"
        busy={tracking}
        error={logError}
        note={episodes.some((e) => e.id == null)
          ? 'Marking downloads the whole show for offline use.'
          : null}
        bulk={{
          kind: 'season',
          watched: watchedAired,
          aired: airedEps.length,
          seasonsNeedingDate: seasonNeedsDate,
          onconfirm: onBulkLog,
        }}
        oncancel={() => (logOpen = false)}
      />
    {/if}

    {#if copyDialog}
      <ChoiceDialog
        heading={copyDialog.owned
          ? `Remove ${copyDialog.src.label} copy?`
          : `Add ${copyDialog.src.label} copy?`}
        subject="{season.show_title} — {season.name}"
        actions={[
          {
            id: copyDialog.owned ? 'remove' : 'add',
            label: copyDialog.owned
              ? `Remove ${copyDialog.src.label} copy`
              : `Add ${copyDialog.src.label} copy`,
          },
        ]}
        note={season.id == null ? 'Adding a copy downloads the whole show for offline use.' : null}
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
        heading={!copyInfo.owned
          ? `Not on ${copyInfo.src.label}`
          : jellyfinIncomplete
            ? `${jellyfinEpisodes} of ${airedEps.length} aired episodes on ${copyInfo.src.label}`
            : `On ${copyInfo.src.label}`}
        subject="{season.show_title} — {season.name}"
        note={jellyfinIncomplete
          ? "This tile mirrors your Jellyfin server's library. Amber means the server holds some of this season's aired episodes, not all of them. It updates on the next scan and can't be set by hand."
          : "This tile mirrors your Jellyfin server's library — a season counts once any of its episodes is there, and turns amber while some are missing. It updates on the next scan and can't be set by hand."}
        actions={[{ id: 'jellyfin', label: 'Jellyfin scan settings', href: '/settings/jellyfin' }]}
        cancelLabel="Close"
        oncancel={() => (copyInfo = null)}
      />
    {/if}
  {/if}
</section>

<style>
  /* This page owns its episode list, the show back-link and the episode-rating
     average. Everything else is shared — see ./detail-page.css and
     ./components/. */

  /* ── Show back-link (inside the hero's info snippet) ─────────────────── */

  .show-link {
    margin: 0 0 0.35rem;
    font-size: 0.85rem;
  }

  .show-link a {
    color: var(--text-dim);
    text-decoration: none;
    transition: color 0.15s ease;
  }

  .show-link a:hover {
    color: var(--accent);
  }

  /* ── Episode grid ─────────────────────────────────────────────────────── */

  /* The up-next grid's shape, at a wider minimum: a 16:9 tile at the 2:3
     grids' --poster-min would be too short to read. */
  .episode-grid {
    --ep-min: 210px;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--ep-min), 1fr));
    gap: 1.1rem 1rem;
  }

  @media (max-width: 599px) {
    .episode-grid {
      --ep-min: 150px;
    }
  }

  /* The link owns the tile-over-caption stack; the card exists for the hover
     selectors below. */
  .ep-card {
    min-width: 0;
  }

  .ep-link {
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }

  /* The up-next card's tile treatment at 16:9. Same placeholder system as the
     poster cells: a stable hue from the show name, shifted per episode. */
  .ep-still {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 18% 26%),
      hsl(var(--poster-hue) 22% 13%)
    );
    border: 1px solid var(--border);
    box-shadow: var(--shadow-card);
    transition:
      transform var(--ease-lift),
      box-shadow var(--ease-lift),
      border-color var(--ease-lift);
  }

  .ep-still img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    transition: transform var(--ease-lift);
  }

  /* Lift the shell, zoom only the artwork — the corner badges are absolute
     children of .ep-still, so they ride the lift untouched (the up-next
     card's treatment). */
  .ep-card:hover .ep-still {
    transform: translateY(-3px);
    box-shadow: var(--shadow-lift);
    border-color: var(--accent);
  }

  .ep-card:hover .ep-still img {
    transform: scale(1.04);
  }

  /* No sticky lift after taps on touch devices. */
  @media (hover: none) {
    .ep-card:hover .ep-still {
      transform: none;
      box-shadow: var(--shadow-card);
      border-color: var(--border);
    }

    .ep-card:hover .ep-still img {
      transform: none;
    }
  }

  .ep-still-code {
    font-size: 1.4rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    color: rgb(255 255 255 / 0.32);
  }

  /* Four badges, four corners, each flush in its corner so the tile's
     overflow clips the outer corner to its radius and only the inner one is
     rounded here. ✓, user rating and ♥ score are the history card's, verbatim
     (./components/HistoryCard.svelte) — the two grids must read as one
     system. The marker is the up-next card's, moved to the corner the other
     three leave free. */
  .check {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.9rem;
    height: 1.9rem;
    border-radius: 0 0 1rem 0;
    background: var(--accent);
    color: #fff;
    font-size: 1rem;
    font-weight: 700;
    line-height: 1;
  }

  .user-rating {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 1.9rem;
    height: 1.9rem;
    padding: 0 0.5rem;
    border-radius: 0 0 0 1rem;
    background: #e8435a;
    color: #fff;
    font-size: 0.95rem;
    font-weight: 600;
    line-height: 1;
  }

  .rating {
    position: absolute;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.25rem 0.5rem;
    border-radius: 0.4rem 0 0 0;
    background: rgb(0 0 0 / 0.35);
    backdrop-filter: blur(4px);
    color: #fff;
    font-size: 0.85rem;
    font-weight: 600;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.85);
  }

  .heart {
    color: #f2555a;
    font-size: 0.95rem;
    line-height: 1;
  }

  .marker {
    position: absolute;
    bottom: 0;
    left: 0;
    padding: 0.25rem 0.55rem;
    border-radius: 0 0.4rem 0 0;
    font-size: 0.7rem;
    font-weight: 600;
    white-space: nowrap;
  }

  .marker.season_finale {
    background: rgb(190 55 55 / 0.8);
    color: #fff;
  }

  .marker.season_premiere {
    background: rgb(55 150 85 / 0.8);
    color: #fff;
  }

  .marker.mid_season_finale {
    background: rgb(220 175 50 / 0.8);
    color: #1f1e1d;
  }

  /* ── Caption below the tile ───────────────────────────────────────────── */

  .ep-meta {
    min-width: 0;
    text-align: center;
  }

  .ep-title {
    margin: 0;
    font-size: 0.85rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .ep-code {
    color: var(--text-dim);
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    margin-right: 0.2rem;
  }

  .ep-facts {
    margin: 0.15rem 0 0;
    font-size: 0.72rem;
    color: var(--text-dim);
  }

  /* Same separator the hero's meta line uses, so the two read as one system. */
  .ep-facts span:not(:first-child)::before {
    content: '·';
    margin: 0 0.35rem;
  }
</style>
