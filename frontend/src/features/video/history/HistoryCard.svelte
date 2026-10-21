<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { formatInstant } from '../../../lib/time.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'

  /** @type {{ entry: import('../common/api.js').HistoryEntry }} */
  let { entry } = $props()

  /**
   * The month bar above already states month and year, so this only needs the
   * day and the time. The month is kept anyway: weekday+day alone renders as
   * "15 Sat" in some locales, which reads as a fragment rather than a date.
   * watched_at is an RFC3339 UTC instant, so this renders in the user's
   * zone (D-038) — that day can differ from the UTC day near midnight.
   * @param {string} ts RFC3339 UTC instant
   */
  function watchedLabel(ts) {
    return formatInstant(ts, {
      weekday: 'short',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  // One shape for both sides of the union, so the markup below branches once
  // here instead of everywhere. The caption is the single line under the
  // poster: "S10xE3 Fifty Shades of Green" for episodes (unpadded, x-separated),
  // the bare title for movies. Show name and release year live in the tooltip.
  //
  // Both hrefs address the target by LOCAL catalog id, never a tmdb id: a
  // history entry exists only for something already materialised here, so
  // clicking through renders from the snapshot and touches no provider. An
  // episode has no id-addressed page, so it is addressed by its season plus its
  // number — the same form the season page's episode list links with.
  const view = $derived.by(() => {
    if (entry.episode) {
      const ep = entry.episode
      const caption = `S${ep.season}xE${ep.number} ${ep.title}`
      return {
        href:
          ep.season_id != null
            ? `/video/episode-details?season_id=${ep.season_id}&episode=${ep.number}`
            : null,
        artKey: ep.show.name,
        caption,
        tooltip: `${ep.show.name} — ${caption}`,
      }
    }
    const movie = entry.movie
    const title = movie?.title ?? ''
    return {
      href: movie ? `/video/movie-details?id=${movie.id}` : null,
      artKey: title,
      caption: title,
      tooltip: movie?.release_date ? `${title} (${movie.release_date.slice(0, 4)})` : title,
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
      <div class="placeholder" style="--poster-hue: {hueFor(view.artKey)}">
        <span>{initialsFor(view.artKey)}</span>
      </div>
    {/if}
    <!-- Every history entry is a recorded watch, so the mark always shows. -->
    <span class="check" aria-label="Watched">✓</span>
    <!-- Only when this user actually rated the target; unrated shows no badge. -->
    {#if entry.user_rating != null}
      <span class="user-rating">{entry.user_rating}</span>
    {/if}
    {#if entry.rating != null}
      <span class="rating"><span class="heart">♥</span> {entry.rating.toFixed(1)}</span>
    {/if}
  </div>

  <div class="meta">
    <h3 class="title" title={view.tooltip}>{view.caption}</h3>
    <p class="sub">{watchedLabel(entry.watched_at)}</p>
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
