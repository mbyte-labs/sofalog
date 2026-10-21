<script>
  import { route } from '@mateothegreat/svelte5-router'
  import WatchlistCard from './WatchlistCard.svelte'
  import { getWatchlist, getWatchlistMatchUsers } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  let filtersOpen = $state(false)

  // The query parameters of GET /video/watchlist — ordering and filtering are
  // the server's job, not the client's.
  /** @type {'added' | 'title' | 'release_date'} */
  let orderBy = $state('added')
  let order = $state('desc')
  let type = $state('all')

  // Matching watchlist (D-028): the ids of the other users whose lists this
  // one is intersected with. Empty means the plain list. The picker offers
  // only the accounts that opted in, which the server also enforces.
  /** @type {number[]} */
  let matchUsers = $state([])

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', users: import('../common/api.js').MatchUser[] }
   *   | { state: 'error' }}
   */
  let peers = $state({ state: 'loading' })

  async function loadPeers() {
    try {
      peers = { state: 'loaded', users: await getWatchlistMatchUsers() }
    } catch {
      peers = { state: 'error' }
    }
  }
  loadPeers()

  /** @param {number} id @param {boolean} on */
  function setMatched(id, on) {
    matchUsers = on ? [...matchUsers, id] : matchUsers.filter((x) => x !== id)
  }

  // "you and Bob both" / "you, Bob and Carol all" — for the empty state.
  const matchedLabels = $derived.by(() => {
    if (peers.state !== 'loaded') return []
    return peers.users.filter((u) => matchUsers.includes(u.id)).map((u) => u.label)
  })
  const matchedPhrase = $derived.by(() => {
    const names = ['you', ...matchedLabels]
    const list =
      names.length <= 2
        ? names.join(' and ')
        : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`
    return `${list} ${matchedLabels.length === 1 ? 'both' : 'all'}`
  })

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').WatchlistEntry[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  // Guards out-of-order responses: rapid filter toggles fire overlapping
  // requests, and a slow earlier response must not overwrite a newer one.
  let seq = 0

  $effect(() => {
    // Read all four synchronously so the effect re-runs when any changes.
    const params = { orderBy, order, type, matchUsers: [...matchUsers] }
    const id = ++seq
    view = { state: 'loading' }
    getWatchlist(params)
      .then((entries) => {
        if (id === seq) view = { state: 'loaded', entries }
      })
      .catch((err) => {
        if (id !== seq) return
        // A picked user switched sharing off since the picker loaded: the
        // server says so as a 422 naming them. Drop them and re-read who is
        // sharing, rather than show an error for a list that still works.
        const gone =
          err instanceof ApiError && err.code === 'validation'
            ? /user (\d+) is not sharing/.exec(err.message)
            : null
        if (gone) {
          setMatched(Number(gone[1]), false)
          loadPeers()
          return
        }
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  // The direction means something different per key, and "Newest first" above
  // an alphabetical list reads as a bug. Only the labels change — the value
  // sent to the server is asc/desc either way.
  // $derived.by, not $derived: the callback's scope resets narrowing, so
  // orderBy keeps its declared union here instead of collapsing to its initial
  // value.
  const orderLabels = $derived.by(() =>
    orderBy === 'title'
      ? { desc: 'Z to A', asc: 'A to Z' }
      : { desc: 'Newest first', asc: 'Oldest first' },
  )
</script>

<section>
  <p class="back-link"><a href="/video/lists" use:route>← Lists</a></p>
  <div class="page-head">
    <h1>Watchlist</h1>
    <button
      class="filters-toggle"
      class:open={filtersOpen}
      aria-expanded={filtersOpen}
      onclick={() => (filtersOpen = !filtersOpen)}
    >
      Filters
      {#if matchUsers.length > 0}
        <!-- A collapsed panel must not hide an active intersection. -->
        <span class="badge" aria-label="{matchUsers.length} matching">{matchUsers.length}</span>
      {/if}
      <span class="chevron">{filtersOpen ? '▴' : '▾'}</span>
    </button>
  </div>

  {#if filtersOpen}
    <div class="filters">
      <label class="field">
        Show
        <select bind:value={type}>
          <option value="all">Movies &amp; Shows</option>
          <option value="movie">Movies only</option>
          <option value="show">Shows only</option>
        </select>
      </label>

      <label class="field">
        Sort by
        <select bind:value={orderBy}>
          <option value="added">Date added</option>
          <option value="title">Title</option>
          <option value="release_date">Release date</option>
        </select>
      </label>

      <label class="field">
        Order
        <select bind:value={order}>
          <option value="desc">{orderLabels.desc}</option>
          <option value="asc">{orderLabels.asc}</option>
        </select>
      </label>

      {#if peers.state === 'loaded' && peers.users.length > 0}
        <div class="match" role="group" aria-label="Matching watchlist">
          <span class="match-label">Matching watchlist</span>
          {#each peers.users as u (u.id)}
            <label class="field check">
              <input
                type="checkbox"
                checked={matchUsers.includes(u.id)}
                onchange={(e) => setMatched(u.id, e.currentTarget.checked)}
              />
              {u.label}
            </label>
          {/each}
          {#if matchUsers.length > 0}
            <button type="button" class="clear" onclick={() => (matchUsers = [])}>Clear</button>
          {/if}
        </div>
      {:else if peers.state === 'loaded'}
        <p class="hint">No one is sharing their watchlist yet.</p>
      {:else if peers.state === 'error'}
        <p class="hint">Could not load who is sharing their watchlist.</p>
      {/if}
    </div>
  {/if}

  {#if view.state === 'loading'}
    <div class="grid" aria-hidden="true">
      {#each Array(12) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if view.state === 'error'}
    <p class="status error">Could not load watchlist ({view.detail})</p>
  {:else if view.entries.length === 0 && matchUsers.length > 0}
    <p class="status">Nothing {matchedPhrase} have on your watchlists.</p>
  {:else if view.entries.length === 0}
    <p class="status">
      Nothing on your watchlist yet. <a href="/video/search" use:route>Find something to watch</a>.
    </p>
  {:else}
    <div class="grid">
      <!-- Keyed on target_type AND id, not the id alone: the list holds at most
           one row per target, but movie 5 and show 5 both exist, so a bare id
           would be a duplicate key the moment both kinds are listed. -->
      {#each view.entries as entry (`${entry.target_type}:${entry.movie?.id ?? entry.show?.id}`)}
        <WatchlistCard {entry} {orderBy} />
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

  .badge {
    padding: 0 0.45rem;
    background: var(--accent);
    color: #fff;
    border-radius: 999px;
    font-size: 0.7rem;
    line-height: 1.5;
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

  /* The user picker: one row of checkboxes after the selects, wrapping. */
  .match {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 1rem;
    flex-basis: 100%;
  }

  .match-label {
    color: var(--text-dim);
  }

  .check {
    color: var(--text);
    cursor: pointer;
  }

  .check input {
    margin: 0;
    accent-color: var(--accent);
  }

  .clear {
    padding: 0;
    background: none;
    border: none;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
  }

  .hint {
    margin: 0;
    flex-basis: 100%;
    color: var(--text-dim);
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

    .match {
      flex-direction: column;
      align-items: stretch;
    }

    /* A checkbox belongs beside its label, not across the row from it. */
    .field.check {
      justify-content: flex-start;
    }
  }

  @media (hover: none) {
    select,
    .filters-toggle,
    .check,
    .clear {
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

  .status a {
    color: var(--accent);
  }

  .status.error {
    color: var(--err);
  }
</style>
