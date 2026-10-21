<script>
  import { tick } from 'svelte'
  import HistoryCard from './HistoryCard.svelte'
  import { getHistory } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'
  import { formatInstant, zonedParts } from '../../../lib/time.js'

  let filtersOpen = $state(false)

  // The query parameters of GET /video/history — ordering and filtering are the
  // server's job, not the client's.
  let order = $state('desc')
  let type = $state('all')

  const PAGE = 48

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').HistoryEntry[], hasMore: boolean }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })
  let loadingMore = $state(false)

  // Guards out-of-order responses: rapid filter toggles fire overlapping
  // requests, and a slow earlier response must not overwrite a newer one —
  // including a load-more response for a filter set that is no longer active.
  let seq = 0

  $effect(() => {
    // Read both synchronously so the effect re-runs when either changes.
    const params = { order, type }
    const id = ++seq
    view = { state: 'loading' }
    getHistory({ ...params, limit: PAGE, offset: 0 })
      .then((entries) => {
        if (id === seq) view = { state: 'loaded', entries, hasMore: entries.length === PAGE }
      })
      .catch((err) => {
        if (id === seq)
          view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  async function loadMore() {
    if (view.state !== 'loaded' || loadingMore) return
    const id = seq
    loadingMore = true
    try {
      // Offset by what is shown, not a page counter — the batch continues the
      // server's (watched_at, id) ordering exactly where the feed left off.
      const batch = await getHistory({ order, type, limit: PAGE, offset: view.entries.length })
      if (id !== seq) return
      // Pin the scroll across the append: the browser otherwise anchors on the
      // just-clicked button and rides the viewport down past the new batch.
      const scrollY = window.scrollY
      view = {
        state: 'loaded',
        entries: [...view.entries, ...batch],
        hasMore: batch.length === PAGE,
      }
      await tick()
      window.scrollTo(0, scrollY)
    } catch {
      // Keep what is already shown; the button stays and works as a retry.
    } finally {
      loadingMore = false
    }
  }

  /**
   * Fold the server-ordered feed into month sections. This is presentation, not
   * derivation: the server guarantees entries sharing a month are contiguous, so
   * one pass over adjacent entries is enough and no re-sorting happens here. A
   * month with nothing watched simply never appears — there is no gap filling.
   *
   * The key comes from the month in the USER'S ZONE (D-038), not from a
   * prefix of the raw string: watched_at is a UTC instant, so an entry just
   * after their midnight on the 1st sits in the previous UTC month and a string prefix would split the
   * section the label says it is in. Ordering by instant still implies ordering
   * by local month, so a single pass stays correct.
   * @param {import('../common/api.js').HistoryEntry[]} entries
   */
  function groupByMonth(entries) {
    /** @type {{ key: string, label: string, entries: typeof entries }[]} */
    const months = []
    for (const entry of entries) {
      const at = zonedParts(new Date(entry.watched_at))
      const key = `${at.year}-${at.month}`
      if (months.at(-1)?.key !== key) {
        months.push({
          key,
          label: formatInstant(entry.watched_at, {
            month: 'long',
            year: 'numeric',
          }),
          entries: [],
        })
      }
      months.at(-1)?.entries.push(entry)
    }
    return months
  }

  // $derived.by, not $derived: the callback's scope resets narrowing, so `view`
  // keeps its full union type here instead of collapsing to its initial value.
  const months = $derived.by(() => (view.state === 'loaded' ? groupByMonth(view.entries) : []))
</script>

<section>
  <div class="page-head">
    <h1>History</h1>
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
        Show
        <select bind:value={type}>
          <option value="all">Movies &amp; Episodes</option>
          <option value="movie">Movies only</option>
          <option value="episode">Episodes only</option>
        </select>
      </label>

      <label class="field">
        Order
        <select bind:value={order}>
          <option value="desc">Newest first</option>
          <option value="asc">Oldest first</option>
        </select>
      </label>
    </div>
  {/if}

  {#if view.state === 'loading'}
    <div aria-hidden="true">
      <div class="skeleton-bar"></div>
      <div class="grid">
        {#each Array(12) as _, i (i)}
          <div class="skeleton"></div>
        {/each}
      </div>
    </div>
  {:else if view.state === 'error'}
    <p class="status error">Could not load history ({view.detail})</p>
  {:else if view.entries.length === 0}
    <p class="status">Nothing watched yet.</p>
  {:else}
    {#each months as month (month.key)}
      <div class="month-bar">{month.label}</div>
      <div class="grid">
        <!-- Keyed on the WATCH id, never the target id: a rewatch is a second
             entry for the same episode, so a target-keyed list would collide. -->
        {#each month.entries as entry (entry.id)}
          <HistoryCard {entry} />
        {/each}
      </div>
    {/each}
    {#if view.hasMore}
      <div class="load-more-row">
        <button class="load-more" onclick={loadMore} disabled={loadingMore}>
          {loadingMore ? 'Loading…' : 'Load more'}
        </button>
      </div>
    {/if}
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

  .month-bar {
    display: flex;
    align-items: center;
    padding: 0.4rem 0.75rem;
    margin-bottom: 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm);
    font-size: 0.85rem;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--poster-min), 1fr));
    gap: 1.25rem 1rem;
    margin-bottom: 2rem;
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

  .load-more-row {
    display: flex;
    justify-content: center;
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

  @media (hover: none) {
    select,
    .filters-toggle,
    .load-more {
      min-height: 2.75rem;
    }
  }

  .grid:last-child {
    margin-bottom: 0;
  }

  /* Loading placeholders mirror the loaded layout: one month bar + a grid. */
  .skeleton-bar {
    height: 2.2rem;
    margin-bottom: 1rem;
    border-radius: var(--radius-sm);
    background: var(--surface);
    border: 1px solid var(--border);
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

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
