<script>
  // Episode details. Addressed by the owning season — ?season_id= once that
  // snapshot row exists, else the SHOW's ?tmdb_id= + ?season= — plus ?episode=,
  // which both forms need. An episode has no id and no snapshot of its own: it
  // is one entry in its season's, so this page inherits the season's
  // materialise / refresh / stale-serve behaviour whole and only a season that
  // has never been viewed can fail on a TMDB outage.
  //
  // The hero, rating bar and page chrome are shared with the other detail pages
  // (./components/, ./detail-page.css) — the hero in its `wide` form, because a
  // still is 16:9 where a poster is 2:3.
  //
  // "Watch now" is here too, but its copies half is READ-ONLY: copies are
  // recorded per movie and per season (D-021), never per episode, so this page
  // borrows its SEASON's and a tap explains that rather than writing anything.
  //
  // Two cast rails, both out of the season snapshot: the SEASON's regular cast
  // (TMDB's "cast" of every episode in it) and this episode's own guest stars.
  //
  // Marking and rating are live. Marking goes through the watch-log dialog, so
  // every entry carries the instant the user names and an already-watched
  // episode can be logged again (D-009). The first mark of an episode whose
  // show is not tracked yet materialises the WHOLE show — one TMDB call per
  // season — so it is awaited behind a pending state: this call really can take
  // seconds and really can fail.

  import { route as routeAction } from '@mateothegreat/svelte5-router'
  import { TMDB_PATH } from '../../../lib/brandMarks.js'
  import { formatDay, localDayOf } from '../common/format.js'
  import { getEpisodeDetails, markEpisodeWatched, rateTarget, clearRating } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'
  import BrandIcon from '../common/components/BrandIcon.svelte'
  import CastRow from './CastRow.svelte'
  import ChoiceDialog from '../common/components/ChoiceDialog.svelte'
  import DetailHero from '../common/components/DetailHero.svelte'
  import DroppedNotice from './DroppedNotice.svelte'
  import HistoryTimeline from './HistoryTimeline.svelte'
  import RatingBar from '../common/components/RatingBar.svelte'
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
  const seasonId = qs.season_id
  const tmdbId = qs.tmdb_id
  const seasonNumber = qs.season
  const episodeNumber = qs.episode

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'missing' }
   *   | { state: 'error', code: string }
   *   | { state: 'loaded', details: import('../common/api.js').EpisodeDetails }}
   */
  let view = $state({ state: 'loading' })

  const addressed = seasonId != null || (tmdbId != null && seasonNumber != null)
  if (!addressed || episodeNumber == null) {
    view = { state: 'missing' }
  } else {
    getEpisodeDetails(
      seasonId != null
        ? { seasonId, episode: episodeNumber }
        : { tmdbId, season: seasonNumber, episode: episodeNumber },
    )
      .then((details) => {
        view = { state: 'loaded', details }
        watched = details.episode.watched
        userRating = details.episode.user_rating ?? null
      })
      .catch((err) => {
        view = { state: 'error', code: err instanceof ApiError ? err.code : 'unreachable' }
      })
  }

  /** @type {number | null} this user's rating, 1-10 */
  let userRating = $state(null)
  let watched = $state(false)
  // One in-flight flag across marking and rating, so a second action cannot
  // race the first — the movie page does the same.
  let tracking = $state(false)
  /** @type {string | null} one-line failure notice under the buttons */
  let trackError = $state(null)
  /** @type {string | null} rating failure, shown by the bar itself */
  let rateError = $state(null)

  /** @param {unknown} err */
  function trackErrorMessage(err) {
    if (err instanceof ApiError && err.code === 'provider_unavailable') {
      return 'TMDB is unreachable right now — try again in a moment. Your library is unaffected.'
    }
    return 'Saving failed — try again.'
  }

  // The episode's own id once its show is tracked, else the show's tmdb_id plus
  // the two numbers — the only handles this page holds before then.
  function trackTarget() {
    if (view.state !== 'loaded') return {}
    const d = view.details
    return d.episode.id != null
      ? { id: d.episode.id }
      : { tmdbId: d.show_tmdb_id, season: d.season_number, episode: d.episode.episode_number }
  }

  // Adopt the ids and show state a write just minted, so a second action on the
  // same page takes the cheap local-id path.
  /** @param {import('../common/api.js').TrackResult} res */
  function adoptTracked(res) {
    if (view.state !== 'loaded') return
    view.details.episode.id = res.target_id
    view.details.episode.watched = true
    // res.show carries the owning show's recomputed status and progress. This
    // page deliberately shows neither: the show's own page is where progress
    // belongs, and repeating it here made a per-episode action report on the
    // whole show. The one show-level thing the page does show — the dropped
    // notice — comes off the details DTO and needs no adopting: `dropped` is
    // sticky (D-010), so a mark cannot clear it.
  }

  // Bumped after a write that adds a watch row, to make HistoryTimeline
  // refetch. A first mark also mints the episode id, which the timeline picks
  // up on its own — Svelte batches the two into one reload, not two.
  let historyToken = $state(0)

  /** Whether the "when did you watch it" dialog is up. */
  let logOpen = $state(false)
  /** @type {string | null} a failed save, shown inside the dialog */
  let logError = $state(null)

  // The button asks before it writes: every watch is dated, and an
  // already-watched episode is still loggable, because a rewatch is a new
  // entry (D-009) rather than a no-op.
  function onMarkWatched() {
    if (view.state !== 'loaded' || tracking) return
    logError = null
    logOpen = true
  }

  /**
   * The dialog was confirmed. NOT optimistic: on an untracked show this call
   * materialises every season and episode, which takes seconds and can 503, so
   * the dialog holds its pending state and keeps what was typed on a failure.
   *
   * @param {{ watchedAt: string, rating: number | null }} watch
   */
  async function onLogWatch({ watchedAt, rating }) {
    if (view.state !== 'loaded' || tracking) return
    tracking = true
    logError = null
    try {
      const res = await markEpisodeWatched({ ...trackTarget(), watchedAt, rating })
      adoptTracked(res)
      watched = true
      // The same request stored it, so the page's own bar must agree.
      if (rating != null) userRating = rating
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
   * The timeline removed a watch. The server recomputes the SHOW's status from
   * the log, so this can move the show as well as the episode.
   *
   * @param {import('../common/api.js').HistoryEntry} _deleted
   * @param {number | null} remaining watches of this episode still in the log,
   *   or null when a later page might hold more — in which case some remain
   */
  function onWatchDeleted(_deleted, remaining) {
    watched = remaining == null || remaining > 0
    if (view.state === 'loaded') view.details.episode.watched = watched
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
        const id = view.details.episode.id
        if (id != null) await clearRating('episode', id)
      } else {
        const res = await rateTarget({ targetType: 'episode', ...trackTarget(), rating: value })
        view.details.episode.id = res.target_id
      }
    } catch (err) {
      userRating = previous
      rateError = trackErrorMessage(err)
    } finally {
      tracking = false
    }
  }

  // ── Owned copies, borrowed from the season (D-021) ────────────────────

  // Which tile was tapped, and whether the season carries it — there is no
  // write here, so no in-flight flag and nothing to disable: the dialog only
  // explains where copies live and offers the way there.
  /** @type {{ src: { id: string, label: string }, owned: boolean } | null} */
  let copyInfo = $state(null)

  /**
   * @param {{ id: string, label: string }} src
   * @param {{ owned: boolean }} s
   */
  function onSelectCopy(src, s) {
    if (view.state !== 'loaded') return
    copyInfo = { src, owned: s.owned }
  }

  // The same vocabulary the season grid and the up-next cards use.
  const MARKER_LABELS = {
    season_premiere: 'Season Premiere',
    mid_season_finale: 'Mid-Season Finale',
    season_finale: 'Season Finale',
  }

  /** @param {number} n */
  const pad = (n) => String(n).padStart(2, '0')

  /** @param {import('../common/api.js').EpisodeDetails} d */
  const title = (d) => d.episode.title ?? `Episode ${d.episode.episode_number}`

  /** @param {import('../common/api.js').EpisodeDetails} d */
  const code = (d) => `S${pad(d.season_number)}E${pad(d.episode.episode_number)}`

  // The way back up, both rungs of it. The season has a local id only once its
  // show is tracked (D-015), so it falls back to the tmdb pair exactly as the
  // show page's season links do; the show rung is tmdb-addressed either way.
  /** @param {import('../common/api.js').EpisodeDetails} d */
  const seasonHref = (d) =>
    d.season_id != null
      ? `/video/season-details?id=${d.season_id}`
      : `/video/season-details?tmdb_id=${d.show_tmdb_id}&season=${d.season_number}`

  // Said in two places on purpose: under the button, and inside the dialog
  // where Save actually is.
  const FIRST_MARK_NOTE =
    'Logging this the first time downloads the whole show for offline use, which can take a moment.'

  /** @param {import('../common/api.js').EpisodeDetails} d */
  const showHref = (d) => `/video/show-details?tmdb_id=${d.show_tmdb_id}`
</script>

<section class="detail">
  {#if view.state === 'loading'}
    <div class="page-head">
      <h1>Episode details</h1>
    </div>
    <p class="status">Loading…</p>
  {:else if view.state === 'missing' || (view.state === 'error' && view.code === 'not_found')}
    <div class="page-head">
      <h1>Episode details</h1>
    </div>
    <p class="status">
      This episode could not be found.
      <a href="/video/search" use:routeAction>Find its show via Search</a>.
    </p>
  {:else if view.state === 'error'}
    <div class="page-head">
      <h1>Episode details</h1>
    </div>
    {#if view.code === 'provider_unavailable'}
      <!-- Only an episode of a season with no snapshot row can land here: one
           whose season has been viewed before is stale-served instead. -->
      <p class="status error">
        This episode's season hasn't been viewed before, and TMDB is unreachable right now —
        previously viewed seasons keep working. Try again later. Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Loading failed ({view.code}).</p>
    {/if}
  {:else}
    {@const details = view.details}
    {@const episode = details.episode}
    <!-- TMDB's official gradient, referenced as fill="url(#tmdb-grad)". -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <DetailHero wide title={title(details)} poster={episode.still} posterFull={episode.still_full}>
      {#snippet info()}
        <!-- The episode belongs to a season inside a show, and the way back up
             to either is part of the page's identity, so it sits above the
             title. Two links rather than one: the separator is outside both,
             so each rung highlights on its own. -->
        <p class="crumbs">
          <a href={showHref(details)} use:routeAction>‹ {details.show_title}</a>
          <span class="crumb-sep">—</span>
          <a href={seasonHref(details)} use:routeAction>{details.season_name}</a>
        </p>
        <h1>{title(details)}</h1>
        <p class="meta-line">
          <span>{code(details)}</span>
          {#if episode.air_date}
            <span>{formatDay(episode.air_date)}</span>
          {/if}
          {#if episode.runtime_min != null}
            <span>{episode.runtime_min} min</span>
          {/if}
        </p>
        {#if episode.marker}
          <p class="marker-line">
            <span class="marker {episode.marker}">{MARKER_LABELS[episode.marker]}</span>
          </p>
        {/if}
      {/snippet}

      {#snippet desc()}
        {#if episode.overview}
          <p class="overview">{episode.overview}</p>
        {:else}
          <p class="overview pending">TMDB has no overview for this episode.</p>
        {/if}
      {/snippet}
    </DetailHero>

    <div class="detail-body">
      <div class="content">
        <!-- The show two rungs up is dropped (D-010), borrowed through the
             season's call. First on the page for the same reason as on the
             season page: it changes how the rest of it reads. -->
        {#if details.show_dropped}
          <DroppedNotice showTitle={details.show_title} href={showHref(details)} />
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
                    {episode.rating != null ? episode.rating.toFixed(1) : '—'}
                  </span>
                  <span class="score-votes pending">
                    {episode.votes != null
                      ? `${episode.votes.toLocaleString()} votes`
                      : 'episode score'}
                  </span>
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

        <!-- The copies are the SEASON's, shown read-only: an episode is never
             itself owned. The streaming half is the SHOW's, two rungs up
             (D-022) — the same listing the season page carries, from the same
             server call, so the two can never disagree. -->
        <WatchNowSection
          copies={details.season_copies}
          managedKinds={details.jellyfin_synced ? ['jellyfin'] : []}
          streaming={details.streaming}
          subject="show"
          readonly
          onselect={onSelectCopy}
        />

        <!-- An episode has no id until its show is tracked, and an untracked
             show has no watch rows to find, so the timeline renders empty
             without a request until the first mark mints one. -->
        <HistoryTimeline
          targetType="episode"
          targetId={episode.id}
          reloadToken={historyToken}
          emptyNote="You have not watched this episode yet."
          ondeleted={onWatchDeleted}
        />

        <div class="section s-actions">
          <div class="actions">
            <!-- Enabled even when watched: the ✓ treatment is the state, and
                 pressing it again logs another dated entry. The pending label
                 lives in the dialog now, which is where Save is. -->
            <button
              class="watch-cta"
              class:done={watched}
              onclick={onMarkWatched}
              disabled={tracking}
              aria-pressed={watched}
              title={watched ? 'Log another watch of this episode' : 'Log a watch of this episode'}
            >
              {watched ? 'Log another watch' : 'Mark As Watched'}
            </button>
          </div>
          {#if trackError}
            <p class="demo-hint" role="alert">{trackError}</p>
          {:else if !details.episode.id}
            <p class="demo-hint">{FIRST_MARK_NOTE}</p>
          {/if}
        </div>

        <!-- Two rails, the two groups TMDB's own episode page shows. "Cast" is
             the SEASON's regular cast — the same people on every episode of
             it — and "Guest stars" is who was credited for this one episode
             beyond them. Both come out of the season snapshot (no extra call),
             carry no episode counts, and share the show's cast photos. -->
        <div class="section s-cast">
          <h2 class="section-label">Cast</h2>
          <CastRow
            cast={details.cast}
            title={details.show_title}
            emptyNote="TMDB lists no regular cast for this season."
          />
        </div>

        <div class="section s-cast">
          <h2 class="section-label">Guest stars</h2>
          <CastRow
            cast={details.guest_stars}
            title={details.show_title}
            emptyNote="TMDB lists no guest stars for this episode."
          />
        </div>

        <!-- The snapshot is the season's, and only as current as its fetch, so
             date it — the other detail pages do the same. -->
        <div class="section s-provenance">
          <p class="row-note pending">
            Snapshot as of {formatDay(localDayOf(details.fetched_at))}.
          </p>
        </div>
      </div>
    </div>

    {#if logOpen}
      <WatchLogDialog
        title="{code(details)} — {title(details)}"
        busy={tracking}
        error={logError}
        initialRating={userRating}
        note={details.episode.id == null ? FIRST_MARK_NOTE : null}
        onconfirm={onLogWatch}
        oncancel={() => (logOpen = false)}
      />
    {/if}

    {#if copyInfo}
      <!-- Nothing to confirm: the subject is the SEASON, deliberately worded
           exactly as the season page words it, and the only action is the way
           there. -->
      <ChoiceDialog
        heading={copyInfo.owned
          ? `Owned on ${copyInfo.src.label}`
          : `Not owned on ${copyInfo.src.label}`}
        subject="{details.show_title} — {details.season_name}"
        note={details.jellyfin_synced && copyInfo.src.id === 'jellyfin'
          ? "This tile mirrors your Jellyfin server's library, per season — it updates on the next scan and can't be set by hand."
          : 'Copies are recorded per season, not per episode — add or remove them from the season page.'}
        actions={[
          { id: 'season', label: `Go to ${details.season_name}`, href: seasonHref(details) },
        ]}
        cancelLabel="Close"
        oncancel={() => (copyInfo = null)}
      />
    {/if}
  {/if}
</section>

<style>
  /* This page owns only its season back-link and the marker badge. Everything
     else is shared — see ./detail-page.css and ./components/. */

  /* ── Show / season breadcrumb (inside the hero's info snippet) ─────────── */

  /* The show and season pages' back-link treatment, carrying two rungs: the
     same dim-to-accent hover, so all three detail pages read as one system. */
  .crumbs {
    margin: 0 0 0.35rem;
    font-size: 0.85rem;
  }

  .crumbs a {
    color: var(--text-dim);
    text-decoration: none;
    transition: color 0.15s ease;
  }

  .crumbs a:hover {
    color: var(--accent);
  }

  /* Outside both anchors, so it neither highlights nor navigates. */
  .crumb-sep {
    margin: 0 0.35rem;
    color: var(--text-dim);
  }

  /* ── Marker badge ─────────────────────────────────────────────────────── */

  /* The season grid's badge, off the tile and onto its own line: the same
     three colours so the two views read as one system. */
  .marker-line {
    margin: 0.5rem 0 0;
  }

  .marker {
    display: inline-block;
    padding: 0.25rem 0.55rem;
    border-radius: 0.4rem;
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
</style>
