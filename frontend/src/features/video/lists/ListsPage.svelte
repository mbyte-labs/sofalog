<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { getLists } from '../common/api.js'
  import { plural } from '../common/format.js'
  import { assetUrl, backdropSrcset } from '../../../lib/images.js'
  import { ApiError } from '../../../lib/api/client.js'

  // The Lists hub: one banner card per list, its newest items' backdrops
  // collaged behind the title and count. The server states the facts (key,
  // counts, backdrops); the title, glyph and link are this map's — a key it
  // does not know is skipped, so a list added server-side cannot break the page.
  const LISTS = {
    watchlist: {
      title: 'Watchlist',
      href: '/video/lists/watchlist',
      // The detail pages' bookmark glyph, filled.
      icon: 'M7 4h10a1 1 0 0 1 1 1v15l-6-4-6 4V5a1 1 0 0 1 1-1z',
    },
    favorites: {
      title: 'Favorites',
      href: '/video/lists/favorites',
      // The detail pages' heart glyph, filled.
      icon: 'M12 20.5l-1.3-1.2C6.1 15.2 3 12.4 3 8.9 3 6.1 5.2 4 8 4c1.6 0 3.1.7 4 1.9C12.9 4.7 14.4 4 16 4c2.8 0 5 2.1 5 4.9 0 3.5-3.1 6.3-7.7 10.4L12 20.5z',
    },
    dropped: {
      title: 'Dropped',
      href: '/video/lists/dropped',
      // A filled circle with a slash knocked out of it.
      icon: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zM7.05 5.64l11.31 11.31-1.41 1.41L5.64 7.05l1.41-1.41z',
    },
  }

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', lists: import('../common/api.js').ListSummary[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  $effect(() => {
    getLists()
      .then((lists) => {
        view = { state: 'loaded', lists: lists.filter((l) => l.key in LISTS) }
      })
      .catch((err) => {
        view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  })

  /** @param {import('../common/api.js').ListSummary} l */
  function breakdown(l) {
    if (l.movies + l.shows === 0) return 'Nothing here yet'
    const parts = []
    if (l.movies) parts.push(plural(l.movies, 'movie', 'movies'))
    if (l.shows) parts.push(plural(l.shows, 'show', 'shows'))
    return parts.join(' · ')
  }
</script>

<section>
  <div class="page-head">
    <h1>Lists</h1>
  </div>

  {#if view.state === 'loading'}
    <div class="grid" aria-hidden="true">
      {#each Array(Object.keys(LISTS).length) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if view.state === 'error'}
    <p class="status error">Could not load lists ({view.detail})</p>
  {:else}
    <div class="grid">
      {#each view.lists as list (list.key)}
        {@const meta = LISTS[list.key]}
        {@const total = list.movies + list.shows}
        <a class="card" href={meta.href} use:route>
          <!-- The collage's layout is picked by how many backdrops there are;
               see the .art-N rules. Decorative — the caption carries the
               meaning. -->
          <div class="art art-{list.backdrops.length}" aria-hidden="true">
            {#each list.backdrops as src, i (src)}
              <img
                src={assetUrl(src)}
                srcset={backdropSrcset(src, list.backdrops_full[i])}
                sizes="(max-width: 767px) 100vw, 30rem"
                alt=""
                loading="lazy"
              />
            {:else}
              <svg class="art-glyph" viewBox="0 0 24 24">
                <path d={meta.icon} fill="currentColor" />
              </svg>
            {/each}
          </div>

          <div class="caption">
            <div class="label">
              <span class="glyph" aria-hidden="true">
                <svg viewBox="0 0 24 24" width="18" height="18">
                  <path d={meta.icon} fill="currentColor" />
                </svg>
              </span>
              <div class="text">
                <h2>{meta.title}</h2>
                <span class="breakdown">{breakdown(list)}</span>
              </div>
            </div>
            <div class="count">
              <span class="count-n">{total}</span>
              <span class="count-unit">{total === 1 ? 'title' : 'titles'}</span>
            </div>
          </div>
        </a>
      {/each}
    </div>
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: 1.25rem;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 22rem), 1fr));
    gap: 1.25rem;
  }

  /* ── Card ────────────────────────────────────────────────────────────── */

  .card {
    position: relative;
    display: block;
    aspect-ratio: 2 / 1;
    overflow: hidden;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--surface);
    box-shadow: var(--shadow-card);
    color: var(--text);
    text-decoration: none;
    transition:
      transform var(--ease-lift),
      box-shadow var(--ease-lift),
      border-color var(--ease-lift);
  }

  .card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  @media (hover: hover) {
    .card:hover {
      transform: translateY(-3px);
      box-shadow: var(--shadow-lift);
      border-color: var(--accent);
    }

    .card:hover img {
      transform: scale(1.05);
    }
  }

  /* ── Backdrop collage ────────────────────────────────────────────────── */

  .art {
    position: absolute;
    inset: 0;
    display: grid;
    gap: 2px;
    background: var(--bg);
  }

  .art img {
    width: 100%;
    height: 100%;
    min-height: 0;
    object-fit: cover;
    display: block;
    transition: transform 0.4s ease;
  }

  .art-2 {
    grid-template-columns: 1fr 1fr;
  }

  /* Three: one large on the left, two stacked on the right. */
  .art-3 {
    grid-template-columns: 3fr 2fr;
    grid-template-rows: 1fr 1fr;
  }

  .art-3 img:first-child {
    grid-row: span 2;
  }

  .art-4 {
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
  }

  /* Empty, or no item has artwork: an accent wash with the list's glyph. */
  .art-0 {
    place-items: center;
    background:
      radial-gradient(circle at 30% 20%, var(--accent-tint-strong), transparent 60%),
      linear-gradient(135deg, var(--surface), var(--bg));
  }

  .art-glyph {
    width: 30%;
    max-width: 7rem;
    margin-bottom: 12%;
    color: var(--accent);
    opacity: 0.25;
  }

  /* ── Caption over a bottom scrim ─────────────────────────────────────── */

  .caption {
    position: absolute;
    inset: auto 0 0 0;
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 1rem;
    padding: 2.5rem 1rem 0.9rem;
    background: linear-gradient(to top, rgb(0 0 0 / 0.85), rgb(0 0 0 / 0.55) 55%, transparent);
  }

  .label {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
  }

  .glyph {
    display: grid;
    place-items: center;
    flex-shrink: 0;
    width: 2.25rem;
    height: 2.25rem;
    border-radius: 999px;
    background: var(--accent-tint-strong);
    border: 1.5px solid var(--accent);
    color: var(--accent-soft);
  }

  .text {
    min-width: 0;
  }

  h2 {
    margin: 0;
    font-size: 1.2rem;
    line-height: 1.2;
    letter-spacing: -0.01em;
  }

  .breakdown {
    display: block;
    color: rgb(255 255 255 / 0.75);
    font-size: 0.8rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .count {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    flex-shrink: 0;
    line-height: 1;
  }

  .count-n {
    font-size: 2rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.02em;
  }

  .count-unit {
    margin-top: 0.2rem;
    color: rgb(255 255 255 / 0.7);
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  /* ── Loading / error ─────────────────────────────────────────────────── */

  .skeleton {
    aspect-ratio: 2 / 1;
    border-radius: var(--radius-md);
    background: var(--surface);
    border: 1px solid var(--border);
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

  .status {
    margin: 0;
    padding: 3rem 1rem;
    text-align: center;
    font-size: 0.9rem;
  }

  .status.error {
    color: var(--err);
  }

  @media (max-width: 599px) {
    .card,
    .skeleton {
      aspect-ratio: 16 / 9;
    }
  }
</style>
