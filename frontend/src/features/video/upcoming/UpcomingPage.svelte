<script>
  import { route } from '@mateothegreat/svelte5-router'
  import UpcomingCard from './UpcomingCard.svelte'
  import { getUpcoming } from '../common/api.js'
  import { buildCards } from './upcoming.js'
  import { todayLocal } from '../common/format.js'
  import { ApiError } from '../../../lib/api/client.js'

  let filtersOpen = $state(false)

  // The query parameters of GET /video/upcoming. The horizon and the kind are
  // the server's filters; `from` is read once per mount — the viewer's local
  // day, so a section headed "Today" agrees with their clock.
  const RANGES = [
    { days: 14, label: 'Next 2 weeks' },
    { days: 30, label: 'Next month' },
    { days: 90, label: 'Next 3 months' },
    { days: 365, label: 'Next year' },
  ]
  let days = $state(90)
  let type = $state('all')
  const today = todayLocal()

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').UpcomingEntry[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  // Guards out-of-order responses: rapid filter changes fire overlapping
  // requests, and a slow earlier response must not overwrite a newer one.
  let seq = 0

  $effect(() => {
    const params = { type, days, from: today }
    const id = ++seq
    view = { state: 'loading' }
    getUpcoming(params)
      .then((entries) => {
        if (id !== seq) return
        view = { state: 'loaded', entries }
      })
      .catch((err) => {
        if (id !== seq) return
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  const cards = $derived.by(() => (view.state === 'loaded' ? buildCards(view.entries, today) : []))

  const emptyNote = $derived.by(() => {
    const range = RANGES.find((r) => r.days === days)?.label.toLowerCase() ?? `next ${days} days`
    switch (type) {
      case 'movie':
        return `No movie on your watchlist is released in the ${range}.`
      case 'episode':
        return `No episode of a show you follow or have listed airs in the ${range}.`
      default:
        return `Nothing scheduled in the ${range}.`
    }
  })
</script>

<section>
  <div class="page-head">
    <h1>Upcoming</h1>
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
        Range
        <select bind:value={days}>
          {#each RANGES as range (range.days)}
            <option value={range.days}>{range.label}</option>
          {/each}
        </select>
      </label>

      <label class="field">
        Show
        <select bind:value={type}>
          <option value="all">Episodes &amp; movies</option>
          <option value="episode">Episodes only</option>
          <option value="movie">Movies only</option>
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
    <p class="status error">Could not load upcoming ({view.detail})</p>
  {:else if view.entries.length === 0}
    <p class="status">
      {emptyNote}
      <a href="/video/lists/watchlist" use:route>Your watchlist</a> is where new entries come from.
    </p>
  {:else}
    <div class="grid">
      <!-- One grid in the server's date order, the date on every poster: a
           day heading would only repeat what each card already says. -->
      {#each cards as card (card.key)}
        <UpcomingCard {card} />
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

  /* The app's poster grid — the same card size as Up Next, Watchlist and
     History; --poster-min shrinks once at 767px and every page follows. */
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

  .status a {
    color: var(--accent-soft);
  }

  .status.error {
    color: var(--err);
  }
</style>
