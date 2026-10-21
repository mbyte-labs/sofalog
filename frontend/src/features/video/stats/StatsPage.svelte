<script>
  // The Stats page (D-039): one call for one period, every section computed
  // on the server from this user's own log, ratings and show states, and
  // rendered here. A section the server answers with null (or an empty list)
  // is hidden; nothing is derived client-side beyond labels and percentages.
  //
  // The period is the server's to resolve on the first load (the current year
  // when it holds a play, else all time) and echoed back; a change through the
  // filter panel goes through the full-page loader, since every section
  // changes at once. The one write on the page — rating a movie that is
  // waiting for one — refetches silently afterwards, so which movie comes up
  // next stays the server's to say.

  import { route } from '@mateothegreat/svelte5-router'
  import './stats.css'
  import HeadlineGrid from './HeadlineGrid.svelte'
  import StatSection from './StatSection.svelte'
  import BannerCard from './BannerCard.svelte'
  import RatingsSection from './RatingsSection.svelte'
  import TopRatedRail from './TopRatedRail.svelte'
  import RatingTrendSection from './RatingTrendSection.svelte'
  import CrowdSection from './CrowdSection.svelte'
  import UnratedSection from './UnratedSection.svelte'
  import RateDialog from './RateDialog.svelte'
  import ActivitySection from './ActivitySection.svelte'
  import DecadesSection from './DecadesSection.svelte'
  import RewatchedList from './RewatchedList.svelte'
  import CompletionSection from './CompletionSection.svelte'
  import GenresSection from './GenresSection.svelte'
  import { getStats, rateMovie } from '../common/api.js'
  import { plural, watchedAgo } from '../common/format.js'
  import { dayOf, today } from '../../../lib/time.js'
  import { ApiError } from '../../../lib/api/client.js'

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', stats: import('../common/api.js').Stats }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })
  // The period the shown page was computed for — null until the first
  // response echoes the server's default.
  /** @type {string | null} */
  let appliedPeriod = $state(null)
  // The period staged in the filter panel; Apply makes it the applied one.
  let pendingPeriod = $state('')
  let filtersOpen = $state(false)

  // Guards out-of-order responses: a slow earlier response must not overwrite
  // a newer one, whether a period switch or the silent refetch after a rating.
  let seq = 0

  /**
   * @param {string | null} period null = let the server pick the default
   * @param {boolean} silent keep the current sections on screen while loading
   */
  function load(period, silent) {
    const id = ++seq
    if (!silent) view = { state: 'loading' }
    getStats(period ? { period } : {})
      .then((stats) => {
        if (id !== seq) return
        view = { state: 'loaded', stats }
        appliedPeriod = stats.period
        pendingPeriod = stats.period
      })
      .catch((err) => {
        if (id !== seq) return
        // A silent refetch that fails keeps what is shown: the dialog already
        // reported the save, and the stale page is still a correct page.
        if (silent && view.state === 'loaded') return
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  }

  // Mount only: no state is read synchronously here, so the effect never
  // re-fires — the response writing appliedPeriod must not trigger a reload.
  $effect(() => {
    load(null, false)
  })

  function toggleFilters() {
    if (!filtersOpen && appliedPeriod != null) pendingPeriod = appliedPeriod
    filtersOpen = !filtersOpen
  }

  function applyFilters() {
    filtersOpen = false
    load(pendingPeriod, false)
  }

  const stats = $derived.by(() => (view.state === 'loaded' ? view.stats : null))
  const period = $derived(stats?.period ?? appliedPeriod ?? 'all')
  const periodLabel = $derived(period === 'all' ? 'All time' : period)
  const isPastYear = $derived(period !== 'all' && Number(period) < Number(today().slice(0, 4)))
  const showFilters = $derived(stats != null && stats.years.length > 0)
  // Nothing watched, nothing rated and — all time — nothing followed.
  const isEmpty = $derived(
    stats != null &&
      stats.headline.plays === 0 &&
      stats.ratings == null &&
      (period !== 'all' || stats.completion == null),
  )

  // ── Rating a waiting movie ─────────────────────────────────────────────
  /** @type {import('../common/api.js').StatsTitle | null} */
  let rateTarget = $state(null)
  let rateBusy = $state(false)
  /** @type {string | null} */
  let rateError = $state(null)

  /** @param {import('../common/api.js').StatsTitle} title */
  function openRate(title) {
    rateTarget = title
    rateError = null
  }

  /** @param {number} rating */
  async function confirmRating(rating) {
    if (!rateTarget || rateBusy) return
    rateBusy = true
    rateError = null
    try {
      await rateMovie({ id: rateTarget.id, rating })
      rateTarget = null
      // Silent: the sections stay while the next unrated movie moves up.
      load(appliedPeriod, true)
    } catch {
      rateError = 'Could not save your rating. Please try again.'
    } finally {
      rateBusy = false
    }
  }
</script>

<section class="stats">
  <div class="page-head">
    <h1>Stats</h1>
    {#if showFilters}
      <button
        class="filters-toggle"
        class:open={filtersOpen}
        aria-expanded={filtersOpen}
        onclick={toggleFilters}
      >
        Filters
        <span class="chevron">{filtersOpen ? '▴' : '▾'}</span>
      </button>
    {/if}
  </div>

  {#if showFilters && filtersOpen && stats}
    <div class="filters">
      <span class="picker-label">Period:</span>
      <div class="segmented" role="group" aria-label="Period">
        <button aria-pressed={pendingPeriod === 'all'} onclick={() => (pendingPeriod = 'all')}>
          All time
        </button>
        {#each stats.years as y (y)}
          <button
            aria-pressed={pendingPeriod === String(y)}
            onclick={() => (pendingPeriod = String(y))}
          >
            {y}
          </button>
        {/each}
      </div>
      <button class="apply" disabled={pendingPeriod === appliedPeriod} onclick={applyFilters}>
        Apply Filters
      </button>
    </div>
  {/if}

  {#if view.state === 'loading'}
    <div class="skeleton-page" aria-hidden="true">
      <div class="skeleton-tiles">
        {#each Array(4) as _, i (i)}
          <div class="skeleton tile"></div>
        {/each}
      </div>
      <div class="skeleton banner"></div>
      <div class="skeleton chart"></div>
      <div class="skeleton chart"></div>
    </div>
    <p class="status">Crunching your numbers…</p>
  {:else if view.state === 'error'}
    <div class="empty">
      <p class="status error">Something went wrong while computing your stats</p>
      <p class="empty-sub">Give it another try.</p>
      <button class="empty-action" onclick={() => load(appliedPeriod, false)}>Retry</button>
    </div>
  {:else if stats && isEmpty}
    <div class="empty">
      <img class="empty-logo" src="./icons/logo-mark.svg" alt="" />
      {#if period === 'all'}
        <p class="status">There are no stats to show yet</p>
        <p class="empty-sub">Watch something and your stats will appear here.</p>
      {:else}
        <p class="status">Nothing watched or rated in {period}</p>
        <p class="empty-sub">Pick another year, or go back to All time.</p>
      {/if}
      <a class="empty-action" href="/video/trending" use:route>Trending shows</a>
    </div>
  {:else if stats}
    <HeadlineGrid headline={stats.headline} label={periodLabel} />

    {#if stats.most_watched_show}
      {@const mw = stats.most_watched_show}
      {@const hours = Math.floor(mw.minutes / 60)}
      <StatSection label="Most watched show" plain>
        <BannerCard
          title={mw.show}
          badge={hours > 0 ? `Most watched · ${plural(hours, 'hour', 'hours')}` : 'Most watched'}
          subtitle="{plural(mw.plays, 'episode play', 'episode plays')} · watched {watchedAgo(
            dayOf(mw.last_played_at),
            stats.today,
          )}"
        />
      </StatSection>
    {/if}

    {#if stats.ratings}
      <RatingsSection ratings={stats.ratings} />
    {/if}

    {#if stats.top_rated.length > 0}
      <TopRatedRail items={stats.top_rated} />
    {/if}

    {#if stats.rating_trend}
      <RatingTrendSection trend={stats.rating_trend} {period} {isPastYear} />
    {/if}

    {#if stats.crowd}
      <CrowdSection crowd={stats.crowd} />
    {/if}

    {#if stats.unrated_movies}
      <UnratedSection unrated={stats.unrated_movies} today={stats.today} onrate={openRate} />
    {/if}

    <ActivitySection activity={stats.activity} {period} {isPastYear} />

    {#if stats.decades}
      <DecadesSection decades={stats.decades} />
    {/if}

    {#if stats.most_rewatched.length > 0}
      <RewatchedList items={stats.most_rewatched} />
    {/if}

    {#if stats.completion}
      <CompletionSection completion={stats.completion} />
    {/if}

    {#if stats.genres}
      <GenresSection genres={stats.genres} {period} />
    {/if}
  {/if}

  {#if rateTarget}
    <RateDialog
      title={rateTarget.title}
      year={rateTarget.year}
      busy={rateBusy}
      error={rateError}
      onconfirm={confirmRating}
      oncancel={() => (rateTarget = null)}
    />
  {/if}
</section>

<style>
  .stats {
    max-width: 64rem;
  }

  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin-bottom: 1.25rem;
  }

  /* The Trending page's toggle and panel. */
  .filters-toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.4rem 0.9rem;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }

  .filters-toggle:hover,
  .filters-toggle.open {
    border-color: var(--accent);
  }

  .chevron {
    color: var(--text-dim);
    font-size: 0.7rem;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.75rem 1rem;
    margin: 0 0 1.5rem;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    font-size: 0.85rem;
  }

  .picker-label {
    color: var(--text-dim);
    font-weight: 600;
  }

  .segmented {
    display: inline-flex;
    flex-wrap: wrap;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .segmented button {
    padding: 0.4rem 0.85rem;
    background: none;
    border: none;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
  }

  .segmented button + button {
    border-left: 1px solid var(--border);
  }

  .segmented button:hover {
    color: var(--text);
  }

  .segmented button[aria-pressed='true'] {
    background: var(--accent);
    color: #fff;
  }

  .segmented button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .apply {
    margin-left: auto;
    padding: 0.4rem 1rem;
    border: none;
    border-radius: var(--radius-md);
    background: var(--accent);
    color: #fff;
    font: inherit;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
  }

  .apply:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* ── Loading ─────────────────────────────────────────────────────────── */

  .skeleton-page {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .skeleton-tiles {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.75rem;
  }

  .skeleton {
    border-radius: var(--radius-lg);
    background: var(--surface);
    border: 1px solid var(--border);
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

  .skeleton.tile {
    height: 5rem;
  }

  .skeleton.banner {
    aspect-ratio: 2 / 1;
    max-height: 14rem;
  }

  .skeleton.chart {
    height: 12rem;
  }

  /* ── Empty / error ───────────────────────────────────────────────────── */

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 2rem 1rem 3rem;
    text-align: center;
  }

  .empty .status {
    padding: 0.5rem 0 0;
    color: var(--text);
    font-size: 1rem;
    font-weight: 600;
  }

  .empty .status.error {
    color: var(--err);
  }

  .empty-logo {
    width: 3.5rem;
    opacity: 0.6;
  }

  .empty-sub {
    margin: 0.25rem 0 1.25rem;
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  .empty-action {
    padding: 0.5rem 1.25rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    font-size: 0.85rem;
    text-decoration: none;
    cursor: pointer;
  }

  .empty-action:hover {
    border-color: var(--accent);
  }

  @media (min-width: 1100px) {
    .skeleton-tiles {
      grid-template-columns: repeat(4, 1fr);
    }
  }

  @media (max-width: 599px) {
    .page-head {
      align-items: stretch;
      flex-direction: column;
    }

    .filters {
      flex-direction: column;
      align-items: stretch;
    }

    .segmented button {
      flex: 1 1 auto;
    }

    .apply {
      margin-left: 0;
    }
  }

  @media (hover: none) {
    .segmented button,
    .filters-toggle,
    .apply,
    .empty-action {
      min-height: 2.75rem;
    }
  }
</style>
