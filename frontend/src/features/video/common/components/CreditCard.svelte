<script>
  // One card for a FOREIGN title marked against the library: the person
  // page's filmography (D-027) and the trending / popular lists (D-034) —
  // the app's poster card, the Watchlist and Upcoming cards' shell, for a
  // title that may or may not have a local row. The library marks are the
  // whole reason it is not a SimilarRow card: a title the catalog holds links
  // by its local id and wears an "In library" chip, and one this user has
  // watched wears the ✓ every other poster grid gives a watched title. The
  // kind chip is the Upcoming card's, because a combined list holds movies
  // and shows side by side. `character` and `episodes` are the person page's
  // alone; `rank` is the ranked lists' — a list position, 1-based, worn
  // bottom-left where nothing else sits.

  import { route } from '@mateothegreat/svelte5-router'
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../../lib/images.js'
  import { hueFor, initialsFor } from '../posterPlaceholder.js'

  /**
   * @type {{
   *   credit: import('../api.js').PersonCredit | import('../api.js').TrendingEntry,
   *   rank?: number | null,
   * }}
   */
  let { credit, rank = null } = $props()

  const view = $derived.by(() => {
    const page = credit.media_type === 'movie' ? 'movie-details' : 'show-details'
    // Local id when the library holds it (the watchlist card's address), else
    // TMDB's (the search card's) — the server says which by nulling `id`.
    const href =
      credit.id != null
        ? `/video/${page}?id=${credit.id}`
        : `/video/${page}?tmdb_id=${credit.tmdb_id}`
    const character = 'character' in credit ? credit.character : null
    const episodes = 'episodes' in credit ? credit.episodes : null
    const sub = [credit.year, character].filter(Boolean).join(' · ')
    return {
      href,
      sub,
      episodes,
      kind: credit.media_type === 'movie' ? 'Movie' : 'Show',
      tooltip: credit.year ? `${credit.title} (${credit.year})` : credit.title,
    }
  })

  /** @param {number} n */
  function episodesLabel(n) {
    return `${n} episode${n === 1 ? '' : 's'}`
  }
</script>

<!-- .credit, not .card: this grid lives inside a .detail page, whose shared
     stylesheet dresses .card as a padded surface box. -->
<article class="credit">
  <a class="card-link" href={view.href} use:route>
    <div class="poster">
      {#if credit.poster}
        <img
          src={assetUrl(credit.poster)}
          srcset={posterSrcset(credit.poster, credit.poster_full)}
          sizes={GRID_POSTER_SIZES}
          alt=""
          loading="lazy"
        />
      {:else}
        <div class="placeholder" style="--poster-hue: {hueFor(credit.title)}">
          <span>{initialsFor(credit.title)}</span>
        </div>
      {/if}
      <div class="corner">
        <span class="chip kind">{view.kind}</span>
        {#if credit.id != null && !credit.watched}
          <!-- Held but not watched; a watched title says so with the ✓
               instead, since watched implies in the library. -->
          <span class="chip library">In library</span>
        {/if}
      </div>
      {#if credit.watched}
        <!-- Movie: any watch in the log. Show: finished (D-012) — the
             watchlist page's meaning of the same ✓. -->
        <span class="check" aria-label="Watched">✓</span>
      {/if}
      {#if credit.rating != null}
        <span class="rating"><span class="heart">♥</span> {credit.rating.toFixed(1)}</span>
      {/if}
      {#if rank != null}
        <span class="rank" aria-label="Rank {rank}">#{rank}</span>
      {/if}
    </div>

    <div class="meta">
      <h3 class="title" title={view.tooltip}>{credit.title}</h3>
      {#if view.sub}
        <p class="sub" title={view.sub}>{view.sub}</p>
      {/if}
      {#if view.episodes != null}
        <p class="sub episodes">{episodesLabel(view.episodes)}</p>
      {/if}
    </div>
  </a>
</article>

<style>
  .credit {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  /* The anchor must not change the card's layout or typography — it exists
     only to make the whole card one click target. */
  .card-link {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }

  /* The poster is the card's body, so the keyboard ring sits on it. */
  .card-link:focus-visible {
    outline: none;
  }

  .card-link:focus-visible .poster {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .poster {
    position: relative;
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-card);
    transition:
      transform var(--ease-lift),
      box-shadow var(--ease-lift),
      border-color var(--ease-lift);
  }

  .poster img,
  .placeholder {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    transition: transform var(--ease-lift);
  }

  /* Lift the shell, zoom only the artwork: the corner badges are absolute
     children of .poster, so they ride the lift untouched. */
  .credit:hover .poster {
    transform: translateY(-3px);
    box-shadow: var(--shadow-lift);
    border-color: var(--accent);
  }

  .credit:hover .poster img,
  .credit:hover .placeholder {
    transform: scale(1.04);
  }

  /* No sticky lift after taps on touch devices. */
  @media (hover: none) {
    .credit:hover .poster {
      transform: none;
      box-shadow: var(--shadow-card);
      border-color: var(--border);
    }

    .credit:hover .poster img,
    .credit:hover .placeholder {
      transform: none;
    }
  }

  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 40% 32%),
      hsl(var(--poster-hue) 45% 14%)
    );
  }

  .placeholder span {
    font-size: 2.2rem;
    font-weight: 700;
    color: rgb(255 255 255 / 0.55);
    letter-spacing: 0.05em;
  }

  @media (max-width: 767px) {
    .placeholder span {
      font-size: 1.6rem;
    }
  }

  /* The chip stack sits flush in the top-left corner; the poster's overflow
     clips its outer corner to the radius, so only the last chip's inner corner
     is rounded — the Upcoming card's arrangement. */
  .corner {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
  }

  .chip {
    padding: 0.25rem 0.55rem;
    font-size: 0.7rem;
    font-weight: 600;
    line-height: 1.3;
    white-space: nowrap;
    color: #fff;
  }

  .chip:last-child {
    border-radius: 0 0 0.4rem 0;
  }

  .chip.kind {
    background: rgb(70 70 80 / 0.85);
  }

  .chip.library {
    background: var(--accent);
  }

  /* Top-right, where the Watchlist card keeps the user's rating: the top-left
     is the chips'. Same size and colour as its ✓. */
  .check {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.9rem;
    height: 1.9rem;
    border-radius: 0 0 0 1rem;
    background: var(--accent);
    color: #fff;
    font-size: 1rem;
    font-weight: 700;
    line-height: 1;
  }

  .rating {
    position: absolute;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.25rem 0.5rem;
    border-radius: 0.4rem 0 0 0;
    background: rgb(0 0 0 / 0.35);
    backdrop-filter: blur(4px);
    color: #fff;
    font-size: 0.85rem;
    font-weight: 600;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.85);
  }

  .heart {
    color: #f2555a;
    font-size: 0.95rem;
    line-height: 1;
  }

  /* Bottom-left, the one free corner: the chips own the top-left, the ✓ the
     top-right and the score the bottom-right. Same glass as the score. */
  .rank {
    position: absolute;
    left: 0;
    bottom: 0;
    padding: 0.25rem 0.5rem;
    border-radius: 0 0.4rem 0 0;
    background: rgb(0 0 0 / 0.35);
    backdrop-filter: blur(4px);
    color: #fff;
    font-size: 0.8rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.85);
  }

  .meta {
    min-width: 0;
    text-align: center;
  }

  .title {
    margin: 0;
    font-size: 0.9rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sub {
    margin: 0.15rem 0 0;
    color: var(--text-dim);
    font-size: 0.75rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sub.episodes {
    font-size: 0.7rem;
    opacity: 0.8;
  }
</style>
