<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { formatInstant } from '../../../lib/time.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'

  /**
   * `orderBy` is the key the page is currently sorted on. The card does not
   * order anything — it only uses it to caption the sub-line with the value the
   * user is sorting by, so the ordering on screen is legible rather than
   * mysterious. `addedVerb` names what `added_at` records on the caller's
   * list — the dropped page passes "Dropped".
   * @type {{
   *   entry: import('../common/api.js').WatchlistEntry,
   *   orderBy: 'added' | 'title' | 'release_date',
   *   addedVerb?: string,
   * }}
   */
  let { entry, orderBy, addedVerb = 'Added' } = $props()

  /**
   * added_at is an RFC3339 UTC instant, so this renders in the user's zone
   * (D-038) — that day can differ from the UTC day near midnight.
   * @param {string} ts
   */
  function addedLabel(ts) {
    return formatInstant(ts, {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
    })
  }

  // One shape for both arms of the union, so the markup below branches once
  // here instead of everywhere. A movie carries `title` and `release_date`, a
  // show `name` and `first_air_date` — the two catalog tables keep their own
  // vocabulary, and this is the one place that difference is spelled out.
  const view = $derived.by(() => {
    const target = entry.movie ?? entry.show
    const title = entry.movie?.title ?? entry.show?.name ?? ''
    const year =
      entry.movie?.release_date?.slice(0, 4) ?? entry.show?.first_air_date?.slice(0, 4) ?? null
    return {
      // Both address their details page by local catalog id — a listed item has
      // a catalog row by definition, because listing it is what created one.
      href: entry.movie
        ? `/video/movie-details?id=${entry.movie.id}`
        : entry.show
          ? `/video/show-details?id=${entry.show.id}`
          : null,
      title,
      year,
      id: target?.id ?? 0,
      // Sorted by date added, the added date is what explains the position;
      // otherwise the release year is the more useful line.
      sub: orderBy === 'added' ? `${addedVerb} ${addedLabel(entry.added_at)}` : year,
      tooltip: year ? `${title} (${year})` : title,
    }
  })
</script>

{#snippet content()}
  <div class="poster">
    {#if entry.poster}
      <img
        src={assetUrl(entry.poster)}
        srcset={posterSrcset(entry.poster, entry.poster_full)}
        sizes={GRID_POSTER_SIZES}
        alt=""
        loading="lazy"
      />
    {:else}
      <div class="placeholder" style="--poster-hue: {hueFor(view.title)}">
        <span>{initialsFor(view.title)}</span>
      </div>
    {/if}
    <!-- Only when this user has actually watched it — for a show, that means
         finished. Watchlisted AND watched is ordinary: the watchlist is an
         independent axis, not a status (D-011). -->
    {#if entry.watched}
      <span class="check" aria-label="Watched">✓</span>
    {/if}
    {#if entry.user_rating != null}
      <span class="user-rating">{entry.user_rating}</span>
    {/if}
    {#if entry.rating != null}
      <span class="rating"><span class="heart">♥</span> {entry.rating.toFixed(1)}</span>
    {/if}
  </div>

  <div class="meta">
    <h3 class="title" title={view.tooltip}>{view.title}</h3>
    {#if view.sub}
      <p class="sub">{view.sub}</p>
    {/if}
  </div>
{/snippet}

<article class="card">
  {#if view.href}
    <a class="card-link" href={view.href} use:route>
      {@render content()}
    </a>
  {:else}
    {@render content()}
  {/if}
</article>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
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
     children of .poster, so they ride the lift untouched — scaling .poster
     itself would distort them and the 1px border. */
  .card:hover .poster {
    transform: translateY(-3px);
    box-shadow: var(--shadow-lift);
    border-color: var(--accent);
  }

  .card:hover .poster img,
  .card:hover .placeholder {
    transform: scale(1.04);
  }

  /* No sticky lift after taps on touch devices. */
  @media (hover: none) {
    .card:hover .poster {
      transform: none;
      box-shadow: var(--shadow-card);
      border-color: var(--border);
    }

    .card:hover .poster img,
    .card:hover .placeholder {
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

  /* All three badges sit flush in a corner; the poster's overflow clips their
     outer corner to its radius, so only the inner corner is rounded here. */
  .check {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.9rem;
    height: 1.9rem;
    border-radius: 0 0 1rem 0;
    background: var(--accent);
    color: #fff;
    font-size: 1rem;
    font-weight: 700;
    line-height: 1;
  }

  .user-rating {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 1.9rem;
    height: 1.9rem;
    padding: 0 0.5rem;
    border-radius: 0 0 0 1rem;
    background: #e8435a;
    color: #fff;
    font-size: 0.95rem;
    font-weight: 600;
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
  }
</style>
