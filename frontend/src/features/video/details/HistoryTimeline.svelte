<script>
  // The details pages' watch log, newest first. Three addressing forms: ONE
  // target (a movie or an episode), ONE SEASON's episodes, or ONE SHOW's
  // across every season. The wider the scope, the more each card has to say
  // about which episode it was — hence episodeLabel below.
  //
  // Layout note, because it constrains everything else: the rail runs ABOVE
  // the cards with a node dot per entry, rather than through them. That is
  // what lets a card be two lines (movie, episode), three (season, which names
  // its episode) or four (armed for deletion) without the rail moving.
  //
  // The section wrapper and its label live here, like WatchNowSection's do:
  // the `.detail .section` / `.section-label` rules are global, from
  // ../detail-page.css, so a child component's markup inside `.detail` still
  // matches them.

  import { getHistory, deleteWatch } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'
  import { zonedFormat } from '../../../lib/time.js'

  /**
   * Exactly one addressing form. `targetType` + `targetId` always travel
   * together: ids are unique only within their own table, so the pair
   * identifies the target and an id alone identifies nothing (movie 5 and
   * episode 5 both exist). `seasonId` and `showId` are the other two — scopes,
   * not targets, since neither a season nor a show is ever itself watched.
   *
   * `ondeleted` fires after a successful removal with the entry that went and
   * how many watches of THAT target are left, because the server recomputes
   * status from the log: the parent's "✓ Watched" control is stale the moment
   * the last one goes. `remainingForTarget` is null when later pages might
   * hold more of the same target, i.e. when the count cannot be known.
   *
   * @type {{
   *   targetType?: 'movie' | 'episode',
   *   targetId?: number | null,
   *   seasonId?: number | null,
   *   showId?: number | null,
   *   emptyNote: string,
   *   reloadToken?: number,
   *   ondeleted?: (
   *     deleted: import('../common/api.js').HistoryEntry,
   *     remainingForTarget: number | null,
   *   ) => void,
   * }}
   */
  let {
    targetType,
    targetId = null,
    seasonId = null,
    showId = null,
    emptyNote,
    reloadToken = 0,
    ondeleted,
  } = $props()

  // How much of the episode each card has to name — exactly as much as the
  // scope leaves ambiguous, and no more. A single-target feed would just
  // repeat the page's own subject; a season feed needs the episode number; a
  // show feed spans seasons, so it needs both. Derived from the scope rather
  // than passed separately, because they are the same fact.
  const episodeLabel = $derived(showId != null ? 'full' : seasonId != null ? 'episode' : 'none')

  const HISTORY_PAGE = 20

  /**
   * @type {{ state: 'loading' } | { state: 'error' }
   *   | { state: 'loaded', entries: import('../common/api.js').HistoryEntry[], hasMore: boolean }}
   */
  let history = $state({ state: 'loading' })
  let loadingMore = $state(false)

  // Guards out-of-order responses: a delete followed by a reloadToken bump can
  // leave two first-page requests in flight, and the slower one must not win.
  let seq = 0

  /** Whichever addressing form this instance was given. @param {number} offset */
  function fetchHistoryPage(offset) {
    return getHistory({
      order: 'desc',
      ...(showId != null
        ? { showId }
        : seasonId != null
          ? { seasonId }
          : { targetType, targetId: targetId ?? undefined }),
      limit: HISTORY_PAGE,
      offset,
    })
  }

  // The first page, reloaded whenever the subject changes or the parent bumps
  // reloadToken after writing a watch. An effect rather than a method the
  // parent calls: a target id can be minted AFTER this component mounts (a
  // page reached by tmdb_id has none until the first track action), and the
  // two changes land in the same tick, which Svelte batches into one run.
  $effect(() => {
    // All three are read so any of them changing re-runs this; reloadToken
    // likewise, though its value is never used.
    const addressed = showId != null || seasonId != null || targetId != null
    void reloadToken
    const mine = ++seq
    confirmingId = null
    deleteError = null
    // No catalog row means no watch rows can reference it: render the empty
    // timeline without a request.
    if (!addressed) {
      history = { state: 'loaded', entries: [], hasMore: false }
      return
    }
    fetchHistoryPage(0)
      .then((batch) => {
        if (mine !== seq) return
        history = { state: 'loaded', entries: batch, hasMore: batch.length === HISTORY_PAGE }
      })
      .catch(() => {
        if (mine === seq) history = { state: 'error' }
      })
  })

  /** @type {number | null} the watch-row id awaiting confirmation; one at a time */
  let confirmingId = $state(null)
  let deleting = $state(false)
  /** @type {string | null} one-line failure notice under the timeline */
  let deleteError = $state(null)

  /** @param {number} id */
  function arm(id) {
    if (deleting) return
    // A second click on the armed card closes it, so the card is its own undo.
    confirmingId = confirmingId === id ? null : id
    deleteError = null
  }

  /**
   * The target a history entry points at, as a comparable key. The pair, never
   * the bare id: movie 5 and episode 5 both exist (D-013).
   *
   * @param {import('../common/api.js').HistoryEntry} e
   */
  const targetKey = (e) => `${e.target_type}:${e.episode?.id ?? e.movie?.id}`

  /**
   * Remove one watch. Awaited, never optimistic: a watch row is real data and
   * the app has no undo, so nothing disappears until the server agrees.
   *
   * @param {import('../common/api.js').HistoryEntry} entry
   */
  async function confirmDelete(entry) {
    if (deleting || history.state !== 'loaded') return
    deleting = true
    deleteError = null
    try {
      await deleteWatch(entry.id)
      // Splice locally rather than refetching: a refetch re-pages from offset
      // 0 and would throw away everything "Load more" had added.
      const entries = history.entries.filter((e) => e.id !== entry.id)
      const hasMore = history.hasMore
      history = { state: 'loaded', entries, hasMore }
      confirmingId = null
      // A later page could hold more watches of the same target, so the count
      // is only knowable once the feed is fully loaded.
      const key = targetKey(entry)
      ondeleted?.(entry, hasMore ? null : entries.filter((e) => targetKey(e) === key).length)
    } catch (err) {
      deleteError =
        err instanceof ApiError && err.code === 'not_found'
          ? 'That watch is already gone — reload to see the current history.'
          : 'Removing the watch failed — try again.'
    } finally {
      deleting = false
    }
  }

  async function loadMoreHistory() {
    if (history.state !== 'loaded' || loadingMore) return
    loadingMore = true
    try {
      const batch = await fetchHistoryPage(history.entries.length)
      history = {
        state: 'loaded',
        entries: [...history.entries, ...batch],
        hasMore: batch.length === HISTORY_PAGE,
      }
    } catch {
      // Keep what is already shown; the button stays and works as a retry.
    } finally {
      loadingMore = false
    }
  }

  // watched_at is an instant, and only this timeline renders one, so the
  // formatter stays here — the day-only formatters live in ../format.js.
  // Hence "15 Aug 2026, 21:05". hour12 false keeps the chip the same width
  // whatever the browser's locale would have chosen.
  /** @type {Intl.DateTimeFormatOptions} */
  const watchedAtOptions = {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }

  /** @param {string} ts RFC3339 UTC instant */
  function formatWatchedAt(ts) {
    // new Date is right here, unlike for a bare day: the string carries its
    // own UTC offset, so this renders the watch in the user's zone (D-038).
    return zonedFormat('en-GB', watchedAtOptions).format(new Date(ts))
  }

  // Display labels for the watch_type provenance value — rendering it is the
  // one thing a client may do with it (D-010: never branch on it).
  const WATCH_TYPE_LABELS = { watch: 'Watched', checkin: 'Check In', scrobble: 'Scrobble' }

  /** @param {number} n */
  const pad = (n) => String(n).padStart(2, '0')
</script>

<div class="section s-history">
  <h2 class="section-label">History</h2>
  <!-- Horizontal timeline, newest on the left. -->
  {#if history.state === 'loading'}
    <div class="card quiet">
      <p>Loading…</p>
    </div>
  {:else if history.state === 'error'}
    <div class="card quiet">
      <p>Loading the watch history failed — reload to retry.</p>
    </div>
  {:else if history.entries.length === 0}
    <div class="card quiet">
      <p>{emptyNote}</p>
    </div>
  {:else}
    <ol class="timeline" aria-label="Watch history, newest first">
      <!-- Keyed on the watch-row id, never the target id: a rewatch is a new
           dated row against the same target. -->
      {#each history.entries as entry (entry.id)}
        {@const armed = confirmingId === entry.id}
        {@const when = formatWatchedAt(entry.watched_at)}
        {@const ep = episodeLabel === 'none' ? null : entry.episode}
        {@const code = ep
          ? episodeLabel === 'full'
            ? `S${pad(ep.season)}E${pad(ep.number)}`
            : `E${pad(ep.number)}`
          : ''}
        <li class="tl-entry">
          <span class="tl-node" aria-hidden="true"></span>
          <!-- The whole card is the delete control, and it keeps its content
               while armed so the confirm below always names the watch it is
               about. -->
          <button
            class="tl-card"
            class:armed
            onclick={() => arm(entry.id)}
            disabled={deleting}
            aria-expanded={armed}
            aria-label={ep
              ? `Remove this watch — ${code} ${ep.title}, ${when}`
              : `Remove this watch — ${when}`}
            title="Remove this watch"
          >
            {#if ep}
              <span class="tl-ep" title={ep.title}>
                <span class="tl-code">{code}</span>
                {ep.title}
              </span>
            {/if}
            <span class="tl-when">{when}</span>
            <span class="tl-kind">{WATCH_TYPE_LABELS[entry.watch_type] ?? entry.watch_type}</span>
            <span class="tl-x" aria-hidden="true">✕</span>
          </button>
          {#if armed}
            <span class="tl-confirm">
              {#if deleting}
                <span class="tl-kind">Deleting…</span>
              {:else}
                <span class="tl-kind">Delete?</span>
                <button class="tl-act danger" onclick={() => confirmDelete(entry)}>Yes</button>
                <button class="tl-act ghost" onclick={() => (confirmingId = null)}>No</button>
              {/if}
            </span>
          {/if}
        </li>
      {/each}
      {#if history.hasMore}
        <li class="tl-entry">
          <span class="tl-node" aria-hidden="true"></span>
          <button class="tl-card load-more" onclick={loadMoreHistory} disabled={loadingMore}>
            {loadingMore ? 'Loading…' : `Load ${HISTORY_PAGE} more`}
          </button>
        </li>
      {/if}
    </ol>
  {/if}
  {#if deleteError}
    <p class="demo-hint" role="alert">{deleteError}</p>
  {/if}
</div>

<style>
  /* ── History timeline ──────────────────────────────────────────────── */

  /* Newest-first horizontal rail, scrolling like the cast row. The rail runs
     ABOVE the cards rather than through them, so a card is free to be two
     lines, three, or four while armed without the rail moving — which is what
     lets one component serve the movie, episode and season pages. */
  .timeline {
    display: flex;
    align-items: flex-start; /* tops align, so the rail stays level */
    margin: 0;
    padding: 0 0 0.5rem;
    list-style: none;
    overflow-x: auto;
    /* Reaching either end of the rail must not hand the swipe to the browser,
       where it becomes back-navigation. */
    overscroll-behavior-x: contain;
  }

  .tl-entry {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.4rem;
    flex-shrink: 0;
    min-width: 11rem;
    max-width: 15rem;
    padding: 0 0.4rem;
  }

  /* Each entry paints its own rail segment: one absolutely-positioned line
     could not span a scroll container's overflow width. Trimmed to a half
     segment at both ends, so a single entry shows just its node, no stubs. */
  .tl-entry::before {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    top: calc(0.3rem - 1px);
    height: 2px;
    background: var(--border);
  }

  .tl-entry:first-child::before {
    left: 50%;
  }

  .tl-entry:last-child::before {
    right: 50%;
  }

  /* The node sits on the rail and is the entry's fixed-height header, which is
     what keeps every card's top edge on the same line. */
  .tl-node {
    position: relative;
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--bg, var(--surface));
  }

  /* The card is the delete control on every page. */
  .tl-card {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    width: 100%;
    padding: 0.45rem 1.5rem 0.5rem 0.6rem; /* right pad clears the ✕ */
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm);
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition:
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  .tl-card:hover:enabled {
    background: var(--accent-tint);
    border-color: var(--accent);
  }

  .tl-card:disabled {
    cursor: default;
  }

  /* Season pages only: which episode this watch was of. */
  .tl-ep {
    max-width: 100%;
    color: var(--text-dim);
    font-size: 0.72rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .tl-code {
    font-weight: 700;
    letter-spacing: 0.04em;
  }

  /* The date is the primary line on every page. */
  .tl-when {
    font-size: 0.85rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .tl-kind {
    color: var(--text-dim);
    font-size: 0.72rem;
  }

  /* The ✕ is what makes the card read as a control rather than a label: quiet
     until the pointer or focus is on it, never absent, so nothing about the
     affordance is hover-only for a touch or keyboard user. */
  .tl-x {
    position: absolute;
    top: 0.4rem;
    right: 0.5rem;
    color: var(--text-dim);
    font-size: 0.7rem;
    transition: color var(--ease-lift);
  }

  .tl-card {
    position: relative;
  }

  .tl-card:hover:enabled .tl-x,
  .tl-card:focus-visible .tl-x {
    color: var(--err);
  }

  /* Armed: the accent bar turns to the danger colour, so the card itself says
     which watch the Yes/No below belongs to. */
  .tl-card.armed {
    border-left-color: var(--err);
    background: var(--accent-tint);
  }

  .tl-card.armed .tl-x {
    color: var(--err);
  }

  /* The confirm hangs below the card, so arming grows the entry downward and
     leaves the rail untouched. */
  .tl-confirm {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }

  .tl-act {
    padding: 0.1rem 0.4rem;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    font: inherit;
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--ease-lift);
  }

  .tl-act.danger {
    background: var(--err);
    color: #1f1e1d;
  }

  .tl-act.danger:hover {
    background: #ea9494;
  }

  .tl-act.ghost {
    background: transparent;
    border-color: var(--border);
    color: var(--text-dim);
  }

  .tl-act.ghost:hover {
    background: rgb(255 255 255 / 0.05);
    color: var(--text);
  }

  /* "Load more" is the rail's final node — card-shaped, button colors, and no
     accent bar because it is not a delete. */
  .load-more {
    align-items: center;
    border-left: 1px solid var(--border);
    background: transparent;
    color: var(--accent);
    font-size: 0.85rem;
    font-weight: 600;
    padding: 0.6rem;
  }

  .load-more:hover:enabled {
    border-color: var(--border);
  }

  .load-more:disabled {
    color: var(--text-dim);
  }

  /* Narrower entries on a phone, so a second card peeks in from the right and
     the rail visibly reads as scrollable. */
  @media (max-width: 599px) {
    .tl-entry {
      min-width: 9rem;
      max-width: 12rem;
    }
  }

  /* Touch: the Yes/No pair confirms a destructive delete — it cannot be a
     14px-tall chip. The ✕ stays an indicator (the whole card is the target). */
  @media (hover: none) {
    .tl-confirm {
      gap: 0.5rem;
    }

    .tl-act {
      min-height: 2.25rem;
      padding: 0 1rem;
      font-size: 0.8rem;
    }

    .tl-x {
      font-size: 0.85rem;
    }
  }
</style>
