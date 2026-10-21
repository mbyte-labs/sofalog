<script>
  // The 1–10 rating widget, shared by the detail pages. Integers with
  // overwrite semantics, matching the real ratings model — no half stars, no
  // history. The component owns the interaction and nothing else: the parent
  // owns the value and does the persisting, via `onrate`.
  //
  // Omitting `onrate` makes it a plain picker whose value the parent reads
  // later — how the watch-log dialog uses it. Every button here is an explicit
  // type="button" for that case: inside a form, a bare button submits it.

  import { ratingColor } from '../ratingColor.js'

  /**
   * @type {{
   *   value?: number | null,
   *   disabled?: boolean,
   *   note?: string | null,
   *   onrate?: (rating: number | null) => void,
   * }}
   */
  let {
    value = $bindable(null),
    // Inert rather than pretending — what a page whose ratings cannot be
    // saved yet needs, and what covers the in-flight moment of a save.
    disabled = false,
    // Replaces the default caption: the reason a disabled bar is disabled, or
    // a failure the parent wants to explain.
    note = null,
    // Fired with the new value after every change, null meaning "unrated".
    // A callback rather than the parent watching `value`, because an effect
    // would also fire on the value the server sent to initialise the bar.
    onrate = undefined,
  } = $props()

  /** @type {number | null} segment index under the pointer, for hover preview */
  let hoverRating = $state(null)

  const displayRating = $derived(hoverRating ?? value ?? 0)

  // Clicking the segment that is already selected clears the rating — the
  // same gesture as the ✕ button, and the same null the parent must persist
  // as a delete rather than a low score.
  /** @param {number | null} next */
  function apply(next) {
    value = next
    hoverRating = null
    onrate?.(next)
  }

  const rateColor = $derived(ratingColor(displayRating))
</script>

<!-- Value above the bar, mirroring the scores' vertical rhythm (label → big
     number → supporting row) so the two blocks align as siblings. -->
<div class="rate" class:disabled style="--rate-color: {rateColor}">
  <div class="rate-value" class:set={displayRating > 0}>
    <span class="rate-num">{displayRating > 0 ? displayRating : '–'}</span>
    <span class="rate-max">/10</span>
    <button
      type="button"
      class="rate-clear"
      class:shown={value != null}
      aria-label="Clear your rating"
      title="Clear your rating"
      {disabled}
      tabindex={value != null && !disabled ? 0 : -1}
      onclick={() => apply(null)}
    >
      ✕
    </button>
  </div>
  <div
    class="rate-bar"
    role="group"
    aria-label="Your rating, 1 to 10"
    onmouseleave={() => (hoverRating = null)}
  >
    {#each { length: 10 }, i}
      <button
        type="button"
        class="rate-seg"
        class:filled={i < displayRating}
        class:preview={hoverRating != null && i < hoverRating}
        aria-pressed={value === i + 1}
        aria-label="Rate {i + 1} out of 10"
        {disabled}
        title={disabled ? note : null}
        onmouseenter={() => (hoverRating = i + 1)}
        onclick={() => apply(value === i + 1 ? null : i + 1)}
      ></button>
    {/each}
  </div>
  <p class="rate-caption" class:pending={note != null}>
    {#if note != null}
      {note}
    {:else if value != null}
      Your rating: <strong>{value}/10</strong>
    {:else}
      What did you think? Tap to rate
    {/if}
  </p>
</div>

<style>
  /* The wrapper carries --rate-color (set inline from the script's ramp), so
       the number and every filled segment recolor together. Capped: a rating
       bar stretched across the whole content width reads as oversized. */
  .rate {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.6rem;
    max-width: 30rem;
  }

  .rate-value {
    display: flex;
    align-items: baseline;
    gap: 0.15rem;
    color: var(--text-dim);
  }

  /* Same scale as .score-value, so the TMDB score and yours read as peers. */
  .rate-num {
    font-size: 1.6rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    line-height: 1;
    transition: color var(--ease-lift);
  }

  .rate-value.set .rate-num {
    color: var(--rate-color);
  }

  .rate-max {
    font-size: 0.85rem;
  }

  .rate-bar {
    display: flex;
    gap: 4px;
    width: 100%;
  }

  .rate-seg {
    flex: 1;
    /* The custom property is a hook for a parent that needs deeper touch
       targets (the watch-log dialog); everywhere else the fallback applies. */
    height: var(--rate-seg-height, 0.85rem);
    padding: 0;
    border: none;
    border-radius: 999px;
    background: var(--border);
    cursor: pointer;
    transition:
      background var(--ease-lift),
      transform var(--ease-lift);
  }

  .rate-seg.filled {
    background: var(--rate-color);
  }

  .rate-seg.preview {
    transform: scaleY(1.3);
  }

  /* A disabled bar still shows a value (a saved rating stays readable) but
     offers no affordance: no pointer, no hover lift, dimmed segments. */
  .rate.disabled .rate-seg {
    cursor: default;
  }

  .rate.disabled .rate-seg.preview {
    transform: none;
  }

  .rate.disabled .rate-bar {
    opacity: 0.55;
  }

  /* Inline after the value; aligns itself, since the row is baseline-laid. */
  .rate-clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    align-self: center;
    width: 1.5rem;
    height: 1.5rem;
    margin-left: 0.5rem;
    flex-shrink: 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: 50%;
    background: none;
    color: var(--text-dim);
    font-size: 0.8rem;
    line-height: 1;
    cursor: pointer;
    opacity: 0;
    visibility: hidden;
    transition:
      opacity var(--ease-lift),
      color var(--ease-lift),
      border-color var(--ease-lift);
  }

  .rate-clear.shown {
    opacity: 1;
    visibility: visible;
  }

  .rate-clear:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .rate-clear:disabled {
    cursor: default;
    opacity: 0;
    visibility: hidden;
  }

  .rate-caption {
    margin: 0.5rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  .rate-caption strong {
    color: var(--text);
  }

  /* Matches the .pending treatment the detail pages use for anything that
     cannot act yet. */
  .rate-caption.pending {
    font-style: italic;
  }

  @media (hover: none) {
    .rate-seg.preview {
      transform: none;
    }

    /* Deeper default segments on touch — the fallback side of the
       --rate-seg-height hook, so a parent's own value still wins. */
    .rate-seg {
      height: var(--rate-seg-height, 1.35rem);
    }

    .rate-clear {
      width: 2.25rem;
      height: 2.25rem;
      font-size: 0.9rem;
    }
  }
</style>
