<script>
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { episodeCode } from './upNext.js'

  // The whole card is one button rather than one link: a tap has two possible
  // meanings here — "I watched this" and "tell me about this" — so it opens the
  // page's chooser, which owns both. The link to the details page lives in that
  // dialog, still a real anchor.
  /**
   * @type {{
   *   entry: import('../common/api.js').UpNextEntry,
   *   onchoose: (entry: import('../common/api.js').UpNextEntry) => void,
   * }}
   */
  let { entry, onchoose } = $props()

  const MARKER_LABELS = {
    season_premiere: 'Season Premiere',
    mid_season_finale: 'Mid-Season Finale',
    season_finale: 'Season Finale',
  }

  const episode = $derived(entry.episode)
  const code = $derived(episodeCode(entry))
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
      <div class="placeholder" style="--poster-hue: {hueFor(entry.show.name)}">
        <span>{initialsFor(entry.show.name)}</span>
      </div>
    {/if}
    {#if episode.rating != null}
      <span class="rating"><span class="heart">♥</span> {episode.rating.toFixed(1)}</span>
    {/if}
    {#if episode.marker || !episode.aired}
      <div class="corner">
        {#if episode.marker}
          <span class="marker {episode.marker}">{MARKER_LABELS[episode.marker]}</span>
        {/if}
        {#if !episode.aired}
          <span class="marker upcoming">Upcoming</span>
        {/if}
      </div>
    {/if}
  </div>

  <div class="meta">
    <h3 class="title" title="{entry.show.name} — {code} {episode.title}">
      {code}
      {episode.title}
    </h3>
  </div>
{/snippet}

<article class="card">
  <button type="button" class="card-button" onclick={() => onchoose(entry)}>
    {@render content()}
  </button>
</article>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
  }

  /* The button must not change the card's layout or typography — it exists
     only to make the whole card one click target — so it is stripped back to
     the anchor it replaced: no chrome, inherited type, full width. */
  .card-button {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-width: 0;
    width: 100%;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    color: inherit;
    text-align: inherit;
    cursor: pointer;
  }

  /* A reset button has no focus ring of its own, and the poster it wraps is
     the thing the keyboard is pointing at. */
  .card-button:focus-visible {
    outline: none;
  }

  .card-button:focus-visible .poster {
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

  /* The marker badges stack in the top-left corner — a season marker first,
     the server-computed "Upcoming" flag underneath when both apply. Only the
     stack's last badge rounds its inner corner, so the column reads as one
     block flush against the poster edge. */
  .corner {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
  }

  .marker {
    padding: 0.25rem 0.55rem;
    font-size: 0.7rem;
    font-weight: 600;
    white-space: nowrap;
  }

  .marker:last-child {
    border-radius: 0 0 0.4rem 0;
  }

  .marker.upcoming {
    background: rgb(70 70 80 / 0.85);
    color: #fff;
  }

  .marker.season_finale {
    background: rgb(190 55 55 / 0.8);
    color: #fff;
  }

  .marker.season_premiere {
    background: rgb(55 150 85 / 0.8);
    color: #fff;
  }

  .marker.mid_season_finale {
    background: rgb(220 175 50 / 0.8);
    color: #1f1e1d;
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
</style>
