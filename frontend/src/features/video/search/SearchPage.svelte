<script>
  import SearchResultCard from './SearchResultCard.svelte'
  import SearchIntro from './SearchIntro.svelte'
  import { searchVideo } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  let query = $state('')
  let type = $state('all')

  /**
   * @type {{ state: 'idle' }
   *   | { state: 'loading' }
   *   | { state: 'loaded', results: import('../common/api.js').SearchResult[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'idle' })

  // Guards out-of-order responses, same as the other pages: typing fires
  // overlapping requests and a slow earlier response must not overwrite a
  // newer one.
  let seq = 0

  $effect(() => {
    // Read both synchronously so the effect re-runs when either changes.
    const q = query.trim()
    const t = type
    const id = ++seq
    if (!q) {
      view = { state: 'idle' }
      return
    }
    view = { state: 'loading' }
    // Debounce: search-as-you-type, but only once the user pauses. The
    // cleanup cancels the pending call on every keystroke.
    const timer = setTimeout(() => {
      searchVideo({ q, type: t })
        .then((results) => {
          if (id === seq) view = { state: 'loaded', results }
        })
        .catch((err) => {
          if (id === seq)
            view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
        })
    }, 300)
    return () => clearTimeout(timer)
  })
</script>

<section>
  <div class="page-head">
    <h1>Search</h1>
  </div>

  <div class="search-bar">
    <!-- svelte-ignore a11y_autofocus — search is this page's single purpose -->
    <input type="search" placeholder="Search movies &amp; shows…" autofocus bind:value={query} />
    <label class="field">
      Type
      <select bind:value={type}>
        <option value="all">All</option>
        <option value="movie">Movies</option>
        <option value="show">Shows</option>
      </select>
    </label>
  </div>

  {#if view.state === 'idle'}
    <SearchIntro />
  {:else if view.state === 'loading'}
    <p class="status">Searching…</p>
  {:else if view.state === 'error'}
    {#if view.detail === 'provider_unavailable'}
      <p class="status error">TMDB is unavailable right now — your library is unaffected.</p>
    {:else}
      <p class="status error">Search failed ({view.detail})</p>
    {/if}
  {:else if view.results.length === 0}
    <p class="status">No results for “{query.trim()}”.</p>
  {:else}
    <div class="grid">
      {#each view.results as result (result.media_type + result.tmdb_id)}
        <SearchResultCard {result} />
      {/each}
    </div>
  {/if}
</section>

<style>
  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }

  .search-bar {
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

  input[type='search'] {
    flex: 1;
    min-width: 12rem;
    padding: 0.4rem 0.6rem;
    background: var(--bg);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font: inherit;
  }

  input[type='search']:focus {
    outline: none;
    border-color: var(--accent);
  }

  .field {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    color: var(--text-dim);
  }

  select {
    padding: 0.3rem 0.5rem;
    background: var(--bg);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font: inherit;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--poster-min), 1fr));
    gap: 1.25rem 1rem;
  }

  @media (max-width: 599px) {
    .search-bar {
      flex-direction: column;
      align-items: stretch;
      gap: 0.6rem;
    }

    .field {
      display: flex;
      justify-content: space-between;
    }
  }

  @media (hover: none) {
    input[type='search'],
    select {
      min-height: 2.75rem;
    }
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
</style>
