<script>
  import { route } from '@mateothegreat/svelte5-router'
  import WatchlistCard from './WatchlistCard.svelte'
  import { getDropped } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  // The dropped page is the favorites page without the kind filter — every
  // entry is a show, since only a show can be dropped. The same grid, the same
  // card; `added_at` is the instant the show was dropped, which is what the
  // card's sub-line says when the list is sorted on it.
  let filtersOpen = $state(false)

  // The query parameters of GET /video/dropped — ordering is the server's job,
  // not the client's.
  /** @type {'added' | 'title' | 'release_date'} */
  let orderBy = $state('added')
  let order = $state('desc')

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').DroppedEntry[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  // Guards out-of-order responses: rapid sort toggles fire overlapping
  // requests, and a slow earlier response must not overwrite a newer one.
  let seq = 0

  $effect(() => {
    // Read both synchronously so the effect re-runs when either changes.
    const params = { orderBy, order }
    const id = ++seq
    view = { state: 'loading' }
    getDropped(params)
      .then((entries) => {
        if (id === seq) view = { state: 'loaded', entries }
      })
      .catch((err) => {
        if (id !== seq) return
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  // The direction means something different per key, and "Newest first" above
  // an alphabetical list reads as a bug. Only the labels change — the value
  // sent to the server is asc/desc either way.
  const orderLabels = $derived.by(() =>
    orderBy === 'title'
      ? { desc: 'Z to A', asc: 'A to Z' }
      : { desc: 'Newest first', asc: 'Oldest first' },
  )
</script>

<section>
  <p class="back-link"><a href="/video/lists" use:route>← Lists</a></p>
  <div class="page-head">
    <h1>Dropped</h1>
    <button
      class="filters-toggle"
      class:open={filtersOpen}
      aria-expanded={filtersOpen}
      onclick={() => (filtersOpen = !filtersOpen)}
    >
      Filters
      <span class="chevron">{filtersOpen ? '▴' : '▾'}</span>
    </button>
  </div>

  {#if filtersOpen}
    <div class="filters">
      <label class="field">
        Sort by
        <select bind:value={orderBy}>
          <option value="added">Date dropped</option>
          <option value="title">Title</option>
          <option value="release_date">First aired</option>
        </select>
      </label>

      <label class="field">
        Order
        <select bind:value={order}>
          <option value="desc">{orderLabels.desc}</option>
          <option value="asc">{orderLabels.asc}</option>
        </select>
      </label>
    </div>
  {/if}

  {#if view.state === 'loading'}
    <div class="grid" aria-hidden="true">
      {#each Array(12) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if view.state === 'error'}
    <p class="status error">Could not load dropped shows ({view.detail})</p>
  {:else if view.entries.length === 0}
    <p class="status">No dropped shows. You can drop a show you've started from its page.</p>
  {:else}
    <div class="grid">
      <!-- Every entry is a show and the list holds one row per show, so the
           show's id alone is a stable key. -->
      {#each view.entries as entry (entry.show?.id)}
        <WatchlistCard {entry} {orderBy} addedVerb="Dropped" />
      {/each}
    </div>
  {/if}
</section>

<style>
  .back-link {
    margin: 0 0 0.35rem;
    font-size: 0.85rem;
  }

  .back-link a {
    color: var(--text-dim);
    text-decoration: none;
    transition: color 0.15s ease;
  }

  .back-link a:hover {
    color: var(--accent);
  }

  .page-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }

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
    gap: 0.75rem 1.25rem;
    margin: 0 0 1.5rem;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    font-size: 0.85rem;
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
    .filters {
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
    select,
    .filters-toggle {
      min-height: 2.75rem;
    }
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
</style>
