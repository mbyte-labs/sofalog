<script>
  import { tick } from 'svelte'
  import { route } from '@mateothegreat/svelte5-router'
  import CreditCard from '../common/components/CreditCard.svelte'
  import { getTrending, getDiscover, getDiscoverOptions } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  // The Trending page (D-034): TMDB's trending list and its Discover list,
  // served live on every view and never stored — the person page's rule with
  // no tracked half, so the whole page is discovery and TMDB being down is a
  // 503 here and nowhere in the library. Every card is the person page's
  // filmography card, marked against the library server-side: a title the
  // catalog holds links by its local id and wears "In library", a watched one
  // the ✓. Every parameter below is the server's to interpret; the client
  // only asks, and appends pages as TMDB hands them out.

  /** @type {'trending' | 'discover'} */
  let list = $state('trending')
  /** @type {'day' | 'week'} */
  let window_ = $state('day')
  /** @type {'all' | 'movie' | 'show'} */
  let type = $state('all')
  /** @type {'popularity' | 'rating' | 'newest' | 'votes'} */
  let sort = $state('popularity')

  // The Discover filters. TMDB applies them, so they only exist on that tab:
  // the trending list offers none, and pretending otherwise would thin its
  // pages after the fetch.
  let filtersOpen = $state(false)
  let voteMin = $state('')
  let voteMax = $state('')
  let votesMin = $state('')
  let language = $state('')
  let yearFrom = $state('')
  let yearTo = $state('')
  /** @type {number[]} */
  let genres = $state([])
  /** @type {number[]} */
  let providers = $state([])

  const activeFilters = $derived(
    [voteMin, voteMax, votesMin, language, yearFrom, yearTo].filter((v) => v !== '').length +
      (genres.length > 0 ? 1 : 0) +
      (providers.length > 0 ? 1 : 0),
  )

  function clearFilters() {
    voteMin = voteMax = votesMin = language = yearFrom = yearTo = ''
    genres = []
    providers = []
  }

  /** @param {number[]} list @param {number} id @param {boolean} on */
  function toggled(list, id, on) {
    return on ? [...list, id] : list.filter((x) => x !== id)
  }

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').TrendingEntry[], page: number, totalPages: number }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })
  let loadingMore = $state(false)

  // Guards out-of-order responses: rapid toggles fire overlapping requests,
  // and a slow earlier response must not overwrite a newer one — including a
  // load-more response for a parameter set that is no longer active.
  let seq = 0

  /** @param {number} page */
  function request(page) {
    return list === 'trending'
      ? getTrending({ window: window_, type, page })
      : getDiscover({
          type,
          sort,
          page,
          voteMin,
          voteMax,
          votesMin,
          language,
          yearFrom,
          yearTo,
          genres,
          providers,
        })
  }

  $effect(() => {
    // Read every parameter synchronously so the effect re-runs when any
    // changes; each change starts over at the first page.
    void [
      list,
      window_,
      type,
      sort,
      voteMin,
      voteMax,
      votesMin,
      language,
      yearFrom,
      yearTo,
      genres,
      providers,
    ]
    const id = ++seq
    view = { state: 'loading' }
    request(1)
      .then((result) => {
        if (id === seq)
          view = {
            state: 'loaded',
            entries: result.entries,
            page: result.page,
            totalPages: result.total_pages,
          }
      })
      .catch((err) => {
        if (id !== seq) return
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  async function loadMore() {
    if (view.state !== 'loaded' || loadingMore) return
    const id = seq
    loadingMore = true
    try {
      const result = await request(view.page + 1)
      if (id !== seq) return
      // Pin the scroll across the append: the browser otherwise anchors on the
      // just-clicked button and rides the viewport down past the new batch.
      const scrollY = window.scrollY
      // A title can sit on two pages when TMDB's ranking shifts between the
      // two calls; the key is the pair, so drop what is already shown.
      const seen = new Set(view.entries.map((e) => `${e.media_type}:${e.tmdb_id}`))
      view = {
        state: 'loaded',
        entries: [
          ...view.entries,
          ...result.entries.filter((e) => !seen.has(`${e.media_type}:${e.tmdb_id}`)),
        ],
        page: result.page,
        totalPages: result.total_pages,
      }
      await tick()
      window.scrollTo(0, scrollY)
    } catch {
      // Keep what is already shown; the button stays and works as a retry.
    } finally {
      loadingMore = false
    }
  }

  // The filter panel's option lists, fetched once the first time Discover is
  // opened. Their failure is only the panel's: the grid and the typed
  // filters still work, and the message says which part is missing.
  /**
   * @type {{ state: 'idle' } | { state: 'loading' }
   *   | { state: 'loaded', options: import('../common/api.js').DiscoverOptions }
   *   | { state: 'error' }}
   */
  let options = $state({ state: 'idle' })

  $effect(() => {
    if (list !== 'discover' || options.state !== 'idle') return
    options = { state: 'loading' }
    getDiscoverOptions()
      .then((o) => (options = { state: 'loaded', options: o }))
      .catch(() => (options = { state: 'error' }))
  })

  // Genre ids are per kind at TMDB — the movie and TV lists overlap only
  // partly — so the choices follow the type filter, and a mixed list offers
  // the union (by id; the shared names are identical).
  const genreOptions = $derived.by(() => {
    if (options.state !== 'loaded') return []
    const { movie, show } = options.options.genres
    if (type === 'movie') return movie
    if (type === 'show') return show
    const seen = new Set()
    return [...movie, ...show]
      .filter((g) => (seen.has(g.id) ? false : (seen.add(g.id), true)))
      .sort((a, b) => a.name.localeCompare(b.name))
  })

  const SORT_LABELS = {
    popularity: 'Most popular',
    rating: 'Highest rated',
    newest: 'Newest',
    votes: 'Most voted',
  }

  const heading = $derived(
    list === 'trending'
      ? window_ === 'day'
        ? 'Trending today'
        : 'Trending this week'
      : `${SORT_LABELS[sort]} on TMDB`,
  )
</script>

<section>
  <div class="page-head">
    <h1>Trending</h1>
    <div class="controls">
      <div class="segmented" role="group" aria-label="Which list">
        <button aria-pressed={list === 'trending'} onclick={() => (list = 'trending')}
          >Trending</button
        >
        <button aria-pressed={list === 'discover'} onclick={() => (list = 'discover')}
          >Discover</button
        >
      </div>
      {#if list === 'trending'}
        <div class="segmented" role="group" aria-label="Time window">
          <button aria-pressed={window_ === 'day'} onclick={() => (window_ = 'day')}>Today</button>
          <button aria-pressed={window_ === 'week'} onclick={() => (window_ = 'week')}
            >This week</button
          >
        </div>
      {/if}
      <label class="field">
        Show
        <select bind:value={type}>
          <option value="all">Movies &amp; Shows</option>
          <option value="movie">Movies only</option>
          <option value="show">Shows only</option>
        </select>
      </label>
      {#if list === 'discover'}
        <label class="field">
          Sort
          <select bind:value={sort}>
            {#each Object.entries(SORT_LABELS) as [value, label] (value)}
              <option {value}>{label}</option>
            {/each}
          </select>
        </label>
        <button
          class="filters-toggle"
          class:open={filtersOpen}
          aria-expanded={filtersOpen}
          onclick={() => (filtersOpen = !filtersOpen)}
        >
          Filters
          {#if activeFilters > 0}
            <span class="badge" aria-label="{activeFilters} active">{activeFilters}</span>
          {/if}
          <span class="chevron">{filtersOpen ? '▴' : '▾'}</span>
        </button>
      {/if}
    </div>
  </div>

  {#if list === 'discover' && filtersOpen}
    <div class="filters">
      <label class="field">
        Score from
        <input
          type="number"
          min="0"
          max="10"
          step="0.5"
          inputmode="decimal"
          placeholder="any"
          bind:value={voteMin}
        />
      </label>
      <label class="field">
        to
        <input
          type="number"
          min="0"
          max="10"
          step="0.5"
          inputmode="decimal"
          placeholder="any"
          bind:value={voteMax}
        />
      </label>
      <label class="field">
        Min votes
        <select bind:value={votesMin}>
          <option value="">Any</option>
          <option value="50">50</option>
          <option value="100">100</option>
          <option value="300">300</option>
          <option value="1000">1 000</option>
          <option value="5000">5 000</option>
        </select>
      </label>
      <label class="field">
        Year from
        <input
          type="number"
          min="1870"
          max="2100"
          inputmode="numeric"
          placeholder="any"
          bind:value={yearFrom}
        />
      </label>
      <label class="field">
        to
        <input
          type="number"
          min="1870"
          max="2100"
          inputmode="numeric"
          placeholder="any"
          bind:value={yearTo}
        />
      </label>
      <label class="field">
        Language
        <select bind:value={language} disabled={options.state !== 'loaded'}>
          <option value="">Any</option>
          {#if options.state === 'loaded'}
            {#each options.options.languages as l (l.code)}
              <option value={l.code}>{l.name}</option>
            {/each}
          {/if}
        </select>
      </label>

      {#if options.state === 'loaded'}
        <div class="picker" role="group" aria-label="Genres">
          <span class="picker-label">Genres <small>(all selected must match)</small></span>
          <div class="picker-options">
            {#each genreOptions as g (g.id)}
              <label class="field check">
                <input
                  type="checkbox"
                  checked={genres.includes(g.id)}
                  onchange={(e) => (genres = toggled(genres, g.id, e.currentTarget.checked))}
                />
                {g.name}
              </label>
            {/each}
          </div>
        </div>
        {#if options.options.providers.length > 0}
          <div class="picker" role="group" aria-label="Streaming services">
            <span class="picker-label">
              Streaming in {options.options.region} <small>(any selected)</small>
            </span>
            <div class="picker-options">
              {#each options.options.providers as p (p.id)}
                <label class="field check">
                  <input
                    type="checkbox"
                    checked={providers.includes(p.id)}
                    onchange={(e) =>
                      (providers = toggled(providers, p.id, e.currentTarget.checked))}
                  />
                  {p.name}
                </label>
              {/each}
            </div>
          </div>
        {/if}
      {:else if options.state === 'error'}
        <p class="picker-note">
          The genre, language and streaming lists are unavailable right now — TMDB did not answer.
          The other filters still work.
        </p>
      {:else}
        <p class="picker-note">Loading genres, languages and streaming services…</p>
      {/if}

      {#if activeFilters > 0}
        <button type="button" class="clear" onclick={clearFilters}>Clear filters</button>
      {/if}
    </div>
  {/if}

  <h2 class="list-label">{heading}</h2>

  {#if view.state === 'loading'}
    <div class="grid" aria-hidden="true">
      {#each Array(20) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if view.state === 'error'}
    {#if view.detail === 'provider_unavailable'}
      <p class="status error">
        TMDB is unavailable right now, and this page is always fetched live — try again in a moment.
        Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Could not load the list ({view.detail})</p>
    {/if}
  {:else if view.entries.length === 0}
    <p class="status">
      {list === 'discover' && activeFilters > 0
        ? 'Nothing on TMDB matches these filters.'
        : 'TMDB lists nothing here right now.'}
    </p>
  {:else}
    <div class="grid">
      <!-- Keyed on the pair, never the id alone: movie 5 and show 5 both exist
           (D-013), and a combined list holds both kinds. The rank is the list
           position — presentation, so it is the client's to number, and it
           keeps counting across appended pages. -->
      {#each view.entries as entry, i (`${entry.media_type}:${entry.tmdb_id}`)}
        <CreditCard credit={entry} rank={i + 1} />
      {/each}
    </div>
    {#if view.page < view.totalPages && view.page < 500}
      <div class="load-more-row">
        <button class="load-more" onclick={loadMore} disabled={loadingMore}>
          {loadingMore ? 'Loading…' : 'Load more'}
        </button>
      </div>
    {/if}
  {/if}

  <!-- No provider disclaimer here: Data Sources is its one home. -->
  <p class="footnote">
    Lists come from TMDB, fetched fresh each time you open this page and never saved.
    <a href="/settings/data-sources" use:route>How sofalog uses TMDB →</a>
  </p>
</section>

<style>
  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
  }

  .controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.6rem 0.9rem;
  }

  /* One pressed segment, the person page's control tokens (surface, border,
     accent when active). */
  .segmented {
    display: inline-flex;
    background: var(--surface);
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

  .field {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  select,
  input[type='number'] {
    padding: 0.35rem 0.5rem;
    background: var(--bg);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font: inherit;
  }

  input[type='number'] {
    width: 5.5rem;
  }

  /* The Watchlist page's toggle and badge. */
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

  .badge {
    min-width: 1.25rem;
    padding: 0 0.35rem;
    border-radius: 999px;
    background: var(--accent);
    color: #fff;
    font-size: 0.7rem;
    font-weight: 700;
    line-height: 1.25rem;
    text-align: center;
  }

  .chevron {
    color: var(--text-dim);
    font-size: 0.7rem;
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.75rem 1.25rem;
    margin: 0 0 1.5rem;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    font-size: 0.85rem;
  }

  /* A picker takes its own full-width row inside the wrapping panel. */
  .picker {
    flex-basis: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .picker-label {
    color: var(--text-dim);
    font-weight: 600;
  }

  .picker-label small {
    font-weight: 400;
    opacity: 0.8;
  }

  .picker-options {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 1rem;
  }

  .check {
    color: var(--text);
  }

  .check input {
    accent-color: var(--accent);
  }

  .picker-note {
    flex-basis: 100%;
    margin: 0;
    color: var(--text-dim);
  }

  .clear {
    padding: 0;
    background: none;
    border: none;
    color: var(--accent);
    font: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }

  .list-label {
    margin: 0 0 1rem;
    color: var(--text-dim);
    font-size: 0.85rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--poster-min), 1fr));
    gap: 1.25rem 1rem;
  }

  /* Loading placeholders mirror the loaded layout: one grid of posters. */
  .skeleton {
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-md);
    background: var(--surface);
    border: 1px solid var(--border);
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

  .status {
    margin: 0;
    padding: 3rem 1rem;
    color: var(--text-dim);
    font-size: 0.9rem;
    text-align: center;
  }

  .status.error {
    color: var(--err);
  }

  .load-more-row {
    display: flex;
    justify-content: center;
    margin-top: 1.5rem;
    /* Never a scroll anchor: appending a page inserts cards ABOVE this row, and
       a browser anchored to the just-clicked button would ride the viewport
       down past the whole new batch to keep the button in place. */
    overflow-anchor: none;
  }

  .load-more {
    padding: 0.5rem 1.5rem;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }

  .load-more:hover:not(:disabled) {
    border-color: var(--accent);
  }

  .load-more:disabled {
    color: var(--text-dim);
    cursor: default;
  }

  .footnote {
    margin: 2rem 0 0;
    color: var(--text-dim);
    font-size: 0.8rem;
    text-align: center;
  }

  .footnote a {
    color: var(--accent);
    text-decoration: none;
  }

  .footnote a:hover {
    text-decoration: underline;
  }

  @media (max-width: 599px) {
    .page-head {
      align-items: stretch;
      flex-direction: column;
    }

    .controls {
      justify-content: space-between;
    }

    .filters {
      flex-direction: column;
      align-items: stretch;
      gap: 0.6rem;
    }

    .filters > .field {
      display: flex;
      justify-content: space-between;
    }

    input[type='number'] {
      width: 7rem;
    }
  }

  @media (hover: none) {
    select,
    input[type='number'],
    .segmented button,
    .filters-toggle,
    .load-more {
      min-height: 2.75rem;
    }
  }
</style>
