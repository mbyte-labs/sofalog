<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { GRID_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'

  // The page builds the display model (it owns the grouping and the collapse
  // of a same-show run, see upcoming.js), so this component only renders it.
  // Same poster card as Up Next, Watchlist and History — one poster size
  // across every video grid — with the kind and the markers stacked in the
  // top-left corner the way the Up Next card stacks its badges, and the date
  // in the bottom-right, where the History card keeps its score.

  /** @type {{ card: import('./upcoming.js').UpcomingCardModel }} */
  let { card } = $props()

  // The Up Next card's vocabulary, so a premiere reads the same on both pages.
  const MARKER_LABELS = {
    season_premiere: 'Season Premiere',
    mid_season_finale: 'Mid-Season Finale',
    season_finale: 'Season Finale',
  }
</script>

{#snippet content()}
  <div class="poster">
    {#if card.poster}
      <img
        src={assetUrl(card.poster)}
        srcset={posterSrcset(card.poster, card.poster_full)}
        sizes={GRID_POSTER_SIZES}
        alt=""
        loading="lazy"
      />
    {:else}
      <div class="placeholder" style="--poster-hue: {hueFor(card.artKey)}">
        <span>{initialsFor(card.artKey)}</span>
      </div>
    {/if}
    <div class="corner">
      <span class="marker kind">{card.chip}</span>
      {#each card.markers as marker (marker)}
        <span class="marker {marker}">{MARKER_LABELS[marker]}</span>
      {/each}
    </div>
    <!-- The date lives on the poster, where the History card keeps its
         score: the grid is one run in date order, with no day bars. -->
    <span class="date" class:soon={card.soon}>{card.dateLabel}</span>
  </div>

  <div class="meta">
    <h3 class="title" title={card.title}>{card.title}</h3>
    <p class="sub" title={card.sub}>{card.sub}</p>
  </div>
{/snippet}

<article class="card">
  {#if card.href}
    <a class="card-link" href={card.href} use:route>
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

  /* The badge stack sits flush in the top-left corner; the poster's overflow
     clips its outer corner to the radius, so only the last badge's inner
     corner is rounded here — the Up Next card's arrangement. */
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
    line-height: 1.3;
    white-space: nowrap;
  }

  .marker:last-child {
    border-radius: 0 0 0.4rem 0;
  }

  /* The kind chip is neutral — the Up Next card's "Upcoming" tone — so the
     coloured markers below it keep their meaning. */
  .marker.kind {
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

  .date {
    position: absolute;
    right: 0;
    bottom: 0;
    padding: 0.25rem 0.55rem;
    border-radius: 0.4rem 0 0 0;
    background: rgb(0 0 0 / 0.45);
    backdrop-filter: blur(4px);
    color: #fff;
    font-size: 0.75rem;
    font-weight: 600;
    white-space: nowrap;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.85);
  }

  /* Today and tomorrow are the two dates worth a glance, so they take the
     accent; every later date stays neutral. */
  .date.soon {
    background: var(--accent);
    text-shadow: none;
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
</style>
