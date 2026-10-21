<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'

  /** @type {{ result: import('../common/api.js').SearchResult }} */
  let { result } = $props()

  const TYPE_LABELS = { movie: 'Movie', show: 'Show' }
</script>

{#snippet content()}
  <div class="poster">
    {#if result.poster}
      <img
        src={assetUrl(result.poster)}
        srcset={posterSrcset(result.poster, result.poster_full)}
        sizes={GRID_POSTER_SIZES}
        alt=""
        loading="lazy"
      />
    {:else}
      <div class="placeholder" style="--poster-hue: {hueFor(result.title)}">
        <span>{initialsFor(result.title)}</span>
      </div>
    {/if}
    <span class="type {result.media_type}">{TYPE_LABELS[result.media_type]}</span>
    {#if result.rating != null}
      <span class="rating"><span class="heart">♥</span> {result.rating.toFixed(1)}</span>
    {/if}
  </div>

  <div class="meta">
    <h3 class="title" title={result.title}>{result.title}</h3>
    {#if result.year}
      <p class="year">{result.year}</p>
    {/if}
  </div>
{/snippet}

<article class="card">
  <!-- Both media types have a detail page; the two live at different paths
       because a movie can also be addressed by its local id and a show cannot. -->
  <a
    class="card-link"
    href="/video/{result.media_type === 'movie'
      ? 'movie-details'
      : 'show-details'}?tmdb_id={result.tmdb_id}"
    use:route
  >
    {@render content()}
  </a>
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

  /* Both badges sit flush in a corner; the poster's overflow clips their outer
     corner to its radius, so only the inner corner is rounded here. */
  .type {
    position: absolute;
    top: 0;
    left: 0;
    padding: 0.25rem 0.55rem;
    border-radius: 0 0 0.4rem 0;
    font-size: 0.7rem;
    font-weight: 600;
    white-space: nowrap;
    color: #fff;
  }

  .type.movie {
    background: rgb(70 110 180 / 0.8);
  }

  .type.show {
    background: rgb(120 80 170 / 0.8);
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

  .year {
    margin: 0.1rem 0 0;
    color: var(--text-dim);
    font-size: 0.8rem;
  }
</style>
