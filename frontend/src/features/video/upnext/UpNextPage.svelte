<script>
  import UpNextCard from './UpNextCard.svelte'
  import ChoiceDialog from '../common/components/ChoiceDialog.svelte'
  import WatchLogDialog from '../common/components/WatchLogDialog.svelte'
  import { getUpNext, markEpisodeWatched } from '../common/api.js'
  import { episodeCode, episodeHref } from './upNext.js'
  import { ApiError } from '../../../lib/api/client.js'

  let filtersOpen = $state(false)

  // The query parameters of GET /video/up-next — sorting and filtering are
  // the server's job, not the client's.
  let orderBy = $state('last_watched')
  let orderDir = $state('desc')
  let onlyWithReleaseDate = $state(false)
  let onlyReleased = $state(false)

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', entries: import('../common/api.js').UpNextEntry[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  // Guards out-of-order responses: rapid filter toggles fire overlapping
  // requests, and a slow earlier response must not overwrite a newer one.
  let seq = 0

  /**
   * Load the list. Also the re-read after a mark: `TrackResult` states the
   * show's status and progress but NOT its next episode, so which episode a
   * card shows — and whether the show is listed at all — stays the server's to
   * state, exactly as the season page's post-bulk re-read leaves its ticks to
   * the server.
   *
   * The four parameters are read synchronously at the top, which is what lets
   * the $effect below track them.
   *
   * @param {boolean} initial false when refreshing an already-rendered list,
   *   which must neither flash the skeleton nor replace a working page with an
   *   error screen over a transient failure
   */
  function load(initial) {
    const params = { orderBy, orderDir, onlyWithReleaseDate, onlyReleased }
    const id = ++seq
    if (initial) view = { state: 'loading' }
    return getUpNext(params)
      .then((entries) => {
        if (id !== seq) return
        view = { state: 'loaded', entries }
        staleAfterMark = false
      })
      .catch((err) => {
        if (id !== seq) return
        if (initial) {
          view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
        } else {
          // The mark landed but the list did not move, so the card still
          // offers the episode that was just watched — and a dated watch
          // always inserts (D-009), so tapping it again would log a second
          // one. Say so rather than swallowing it.
          staleAfterMark = true
        }
      })
  }

  $effect(() => {
    // Braces, not a concise body: an arrow returning load()'s promise would be
    // read as the effect's teardown function.
    load(true)
  })

  // ── Marking an episode from a card ─────────────────────────────────────────

  /** @type {import('../common/api.js').UpNextEntry | null} the entry whose dialogs are up */
  let chosen = $state(null)
  /** Whether the chooser has handed off to the watch dialog. */
  let logOpen = $state(false)
  /** One write at a time; also the dialog's `busy`. */
  let marking = $state(false)
  /** @type {string | null} a failed save, shown inside the watch dialog */
  let logError = $state(null)
  /** Whether the post-mark refresh failed, leaving the grid one action stale. */
  let staleAfterMark = $state(false)
  /**
   * @type {HTMLElement | null} the card that opened the dialogs. Each dialog
   *   restores focus to whatever was active when IT mounted, which is not
   *   enough here: the chooser hands off to the watch dialog, and whether the
   *   handoff captures the card or the chooser's own about-to-be-detached
   *   button depends on the order Svelte mounts and destroys the two branches.
   *   Remembering it here makes the keyboard round trip end on the card either
   *   way — focusing a detached node is a no-op, so a dialog's own restore
   *   cannot undo this one.
   */
  let opener = null

  /**
   * A tap on a card means one of two things — "I watched this" and "tell me
   * about this" — so the card asks which. Only the second is a navigation, and
   * it stays a real link inside the dialog.
   *
   * @param {import('../common/api.js').UpNextEntry} entry
   */
  function onChoose(entry) {
    if (marking) return
    opener = /** @type {HTMLElement | null} */ (document.activeElement)
    logError = null
    logOpen = false
    chosen = entry
  }

  const chooserActions = $derived.by(() => {
    if (chosen == null) return []
    /** @type {{ id: string, label: string, href?: string, secondary?: boolean }[]} */
    const actions = [{ id: 'watch', label: 'Mark as watched' }]
    const href = episodeHref(chosen)
    // Absent only when the season row is gone, which the catalog write order
    // makes unreachable; marking still works there, since it needs nothing but
    // the episode's own id.
    if (href != null) {
      actions.push({ id: 'details', label: 'Episode details', href, secondary: true })
    }
    return actions
  })

  /** @param {string} id */
  function onChooserAction(id) {
    if (id === 'watch') logOpen = true
  }

  function closeDialogs() {
    logOpen = false
    chosen = null
    opener?.focus()
  }

  /** @param {{ watchedAt: string, rating: number | null }} watch */
  async function onLogWatch({ watchedAt, rating }) {
    if (chosen == null || marking) return
    marking = true
    logError = null
    try {
      // Always the LOCAL episodes.id: an up-next entry exists only for a show
      // already materialised here, so this write never touches TMDB.
      await markEpisodeWatched({ id: chosen.episode.id, watchedAt, rating })
      // Refreshed before the dialog closes, so the grid never shows the
      // episode that was just marked and its scrim keeps the cards inert
      // until the answer lands.
      await load(false)
      closeDialogs()
    } catch {
      // No provider_unavailable arm, unlike the details pages': the
      // id-addressed path resolves locally, so that message is unreachable
      // from here. A 422 would mean clock skew — the dialog already refuses a
      // future instant client-side.
      logError = 'Saving failed — try again.'
    } finally {
      marking = false
    }
  }
</script>

<section>
  <div class="page-head">
    <h1>Up Next To Watch</h1>
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
        Order by
        <select bind:value={orderBy}>
          <option value="last_watched">Last Watched</option>
          <option value="show_name">Show Name</option>
          <option value="air_date">Episode Release Date</option>
        </select>
      </label>

      <label class="field">
        Order
        <select bind:value={orderDir}>
          <option value="asc">Asc</option>
          <option value="desc">Desc</option>
        </select>
      </label>

      <label class="field check">
        <input type="checkbox" bind:checked={onlyWithReleaseDate} />
        Only episodes with release date
      </label>

      <label class="field check">
        <input type="checkbox" bind:checked={onlyReleased} />
        Only released episodes
      </label>
    </div>
  {/if}

  {#if staleAfterMark}
    <p class="notice" role="status">
      Watch saved, but the list could not refresh — reload to see what is next.
    </p>
  {/if}

  {#if view.state === 'loading'}
    <div class="grid" aria-hidden="true">
      {#each Array(12) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if view.state === 'error'}
    <p class="status error">Could not load up next ({view.detail})</p>
  {:else if view.entries.length === 0}
    <p class="status">Nothing up next — every tracked show is caught up.</p>
  {:else}
    <div class="grid">
      <!-- Keyed by SHOW, not by episode: a card advances to the next episode
           in place after a mark, so the component is reused and the button the
           watch dialog must hand focus back to survives the refetch. -->
      {#each view.entries as entry (entry.show.id)}
        <UpNextCard {entry} onchoose={onChoose} />
      {/each}
    </div>
  {/if}

  <!-- One instance for the whole grid, and never both at once: two mounted
       focus traps would fight over the page. The chooser hands off by setting
       logOpen, which swaps it for the dialog that owns the write. -->
  {#if chosen && !logOpen}
    <ChoiceDialog
      heading="{episodeCode(chosen)} {chosen.episode.title}"
      subject={chosen.show.name}
      note={chosen.episode.aired ? null : "This episode hasn't aired yet, as far as TMDB knows."}
      actions={chooserActions}
      onconfirm={onChooserAction}
      oncancel={closeDialogs}
    />
  {:else if chosen}
    <!-- The episode page's dialog, in the same single mode: one instant and an
         optional rating in one transaction. No initialRating — the entry's
         `rating` is TMDB's community score, not this user's, and an up-next
         episode is by definition unrated. -->
    <WatchLogDialog
      title="{chosen.show.name} — {episodeCode(chosen)} {chosen.episode.title}"
      busy={marking}
      error={logError}
      onconfirm={onLogWatch}
      oncancel={closeDialogs}
    />
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

  .check input {
    margin: 0;
    accent-color: var(--accent);
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

    .field:not(.check) {
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

  .status.error {
    color: var(--err);
  }

  /* A mark that saved against a list that then failed to refresh: the grid
     below is one action stale, so this sits above it rather than replacing it. */
  .notice {
    margin: 0 0 1rem;
    padding: 0.6rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--err);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 0.85rem;
  }
</style>
