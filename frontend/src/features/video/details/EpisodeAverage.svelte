<script>
  // The average of this user's episode ratings, shown in a "Your rating" slot
  // as a READ, not a control. A season is not rateable (D-020), so the season
  // page's slot holds only this; the show page gives it a card of its own
  // beside the show's RatingBar. The parent supplies the
  // numbers — the season page derives them from its episode list, the show
  // page reads the server's show-wide pair — and the two captions, since only
  // the parent knows what the mean is over.
  //
  // Wears the RatingBar's colour ramp so "your" numbers read the same on every
  // detail page; the heart is recoloured by it, which is what keeps it apart
  // from the white-on-poster ♥ of a TMDB score.

  import { ratingColor } from '../common/ratingColor.js'

  /**
   * @type {{
   *   value: number | null,
   *   rated: number,
   *   total: number,
   *   note: string,
   *   emptyNote: string,
   * }}
   */
  let {
    // The mean, or null when nothing is rated.
    value,
    // How many ratings went into it, and how many episodes there are.
    rated,
    total,
    // Under the figure when set / when null: what the mean is over.
    note,
    emptyNote,
  } = $props()

  const color = $derived(value != null ? ratingColor(value) : 'var(--text-dim)')
</script>

<!-- The wrapper carries --avg-color, so the heart and the number recolour
     together — the RatingBar's own trick. -->
<div class="avg" style="--avg-color: {color}">
  <div class="avg-line">
    <svg class="avg-heart" viewBox="0 0 24 24" width="28" height="28" aria-hidden="true">
      <path
        d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"
        fill={value != null ? 'currentColor' : 'none'}
        stroke="currentColor"
        stroke-width={value != null ? 0 : 1.6}
      />
    </svg>
    <span class="avg-value" class:set={value != null}>
      {value != null ? value.toFixed(1) : '–'}
    </span>
    <span class="avg-max">/10</span>
  </div>
  {#if value != null}
    <p class="avg-count">
      <strong>{rated}</strong> of {total}
      {total === 1 ? 'episode' : 'episodes'} rated
    </p>
    <p class="avg-caption pending">{note}</p>
  {:else}
    <p class="avg-caption pending">{emptyNote}</p>
  {/if}
</div>

<style>
  .avg {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.45rem;
  }

  .avg-line {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    color: var(--avg-color);
  }

  .avg-heart {
    align-self: center;
    flex-shrink: 0;
  }

  /* Same scale as .score-value and the RatingBar's number, so the TMDB score
     and yours read as peers. */
  .avg-value {
    font-size: 1.6rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    line-height: 1;
    color: var(--text-dim);
  }

  .avg-value.set {
    color: var(--avg-color);
  }

  .avg-max {
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  .avg-count {
    margin: 0;
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  .avg-count strong {
    color: var(--text);
  }

  /* Matches the RatingBar's italic note treatment for "cannot act" copy;
     .pending (detail-page.css) supplies the dim colour. */
  .avg-caption {
    margin: 0;
    font-size: 0.85rem;
    font-style: italic;
  }
</style>
