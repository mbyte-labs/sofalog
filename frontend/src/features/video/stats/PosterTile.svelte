<script>
  // A poster tile with the user's rating pill — the details pages' "Similar"
  // card re-scoped outside .detail, for the highest-rated rail.

  import { route } from '@mateothegreat/svelte5-router'
  import { RAIL_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { ratingColor } from '../common/ratingColor.js'
  import { titleHref, titleName } from './labels.js'

  /** @type {{ title: import('../common/api.js').StatsTitle, rating: number }} */
  let { title, rating } = $props()

  const name = $derived(titleName(title))
</script>

<a
  class="tile"
  href={titleHref(title)}
  use:route
  style="--poster-hue: {hueFor(name)}; --rate-color: {ratingColor(rating)}"
>
  <span class="tile-poster">
    {#if title.poster}
      <img
        src={assetUrl(title.poster)}
        srcset={posterSrcset(title.poster, title.poster_full)}
        sizes={RAIL_POSTER_SIZES}
        alt=""
        loading="lazy"
      />
    {:else}
      <span class="tile-initials">{initialsFor(name)}</span>
    {/if}
    <span class="tile-rating" aria-label="Your rating {rating}">
      <span class="heart">♥</span>
      {rating}
    </span>
  </span>
  <span class="tile-name" title={name}>{name}</span>
  {#if title.year}
    <span class="tile-meta">{title.year}</span>
  {/if}
</a>

<style>
  .tile {
    flex: none;
    width: 108px;
    scroll-snap-align: start;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    color: var(--text);
    text-decoration: none;
  }

  .tile-poster {
    position: relative;
    display: grid;
    place-items: center;
    aspect-ratio: 2 / 3;
    overflow: hidden;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 40% 32%),
      hsl(var(--poster-hue) 45% 14%)
    );
    box-shadow: var(--shadow-card);
    transition:
      transform var(--ease-lift),
      box-shadow var(--ease-lift),
      border-color var(--ease-lift);
  }

  @media (hover: hover) {
    .tile:hover .tile-poster {
      transform: translateY(-3px);
      box-shadow: var(--shadow-lift);
      border-color: var(--accent);
    }
  }

  .tile:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-md);
  }

  .tile-poster img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .tile-initials {
    font-size: 1.4rem;
    font-weight: 700;
    color: rgb(255 255 255 / 0.7);
  }

  .tile-rating {
    position: absolute;
    right: 0.3rem;
    bottom: 0.3rem;
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    padding: 0.1rem 0.4rem;
    border-radius: 999px;
    background: rgb(0 0 0 / 0.55);
    backdrop-filter: blur(4px);
    font-size: 0.75rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }

  .heart {
    color: var(--rate-color);
  }

  .tile-name {
    font-size: 0.8rem;
    font-weight: 600;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile-meta {
    color: var(--text-dim);
    font-size: 0.72rem;
  }

  @media (min-width: 1100px) {
    .tile {
      width: 132px;
    }
  }
</style>
