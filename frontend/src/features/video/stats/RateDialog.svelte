<script>
  // "Rate" for a watched movie waiting for a rating: the WatchLogDialog's
  // modal mechanics (scrim-as-button, Escape, focus trap, scroll lock, focus
  // back to the opener) around one RatingBar. The parent owns the request and
  // passes `busy` / `error`; the dialog stays open on a failure so a second
  // try costs nothing.

  import RatingBar from '../common/components/RatingBar.svelte'

  /**
   * @type {{
   *   title: string,
   *   year?: number | null,
   *   busy?: boolean,
   *   error?: string | null,
   *   onconfirm: (rating: number) => void,
   *   oncancel: () => void,
   * }}
   */
  let { title, year = null, busy = false, error = null, onconfirm, oncancel } = $props()

  /** @type {number | null} */
  let rating = $state(null)

  /** @type {HTMLFormElement | undefined} */
  let panelEl = $state()
  const opener = /** @type {HTMLElement | null} */ (document.activeElement)

  // The panel takes focus (the bar's segments are buttons, so nothing pops
  // open), which is what makes Escape and the trap reachable at once.
  $effect(() => {
    panelEl?.focus()
  })

  $effect(() => {
    return () => opener?.focus()
  })

  $effect(() => {
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previous
    }
  })

  /** @param {SubmitEvent} event */
  function confirm(event) {
    event.preventDefault()
    // A value below 1 is ignored — the bar cannot produce one, but Save is
    // disabled until a value exists all the same.
    if (busy || rating == null || rating < 1) return
    onconfirm(rating)
  }

  function cancel() {
    if (busy) return
    oncancel()
  }

  /** @param {KeyboardEvent} event */
  function onWindowKey(event) {
    if (event.key === 'Escape') {
      event.preventDefault()
      cancel()
    }
  }

  /** @param {KeyboardEvent} event */
  function onPanelKey(event) {
    if (event.key !== 'Tab' || panelEl == null) return
    const stops = /** @type {HTMLElement[]} */ ([
      ...panelEl.querySelectorAll('button, input, [href], select, textarea'),
    ]).filter(
      (el) =>
        !(/** @type {HTMLButtonElement} */ (el).disabled) && el.getAttribute('tabindex') !== '-1',
    )
    if (stops.length === 0) return
    const first = stops[0]
    const last = stops[stops.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="rd" role="dialog" aria-modal="true" aria-labelledby="rd-heading">
  <button type="button" class="rd-scrim" aria-label="Cancel" onclick={cancel}></button>

  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <form
    class="rd-panel"
    bind:this={panelEl}
    tabindex="-1"
    onsubmit={confirm}
    onkeydown={onPanelKey}
  >
    <h2 id="rd-heading">Rate</h2>
    <p class="rd-subject">{title}{year ? ` (${year})` : ''}</p>

    <div class="rd-rating">
      <span class="rd-field-label">Your rating</span>
      <RatingBar bind:value={rating} disabled={busy} />
    </div>

    {#if error}
      <p class="rd-error" role="alert">{error}</p>
    {/if}

    <div class="rd-actions">
      <button type="submit" class="rd-save" disabled={busy || rating == null}>
        {busy ? 'Saving…' : 'Save'}
      </button>
      <button type="button" class="rd-cancel" onclick={cancel} disabled={busy}>Cancel</button>
    </div>
  </form>
</div>

<style>
  .rd {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  .rd-scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: rgb(0 0 0 / 0.6);
    backdrop-filter: blur(4px);
    cursor: default;
  }

  .rd-panel {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: min(26rem, 100%);
    max-height: calc(100dvh - 2rem);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 1.15rem 1.2rem 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lift);
  }

  .rd-panel:focus {
    outline: none;
  }

  h2 {
    margin: 0;
    font-size: 1.05rem;
  }

  .rd-subject {
    margin: -0.85rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  .rd-rating {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .rd-field-label {
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  .rd-error {
    margin: 0;
    color: var(--err);
    font-size: 0.8rem;
  }

  .rd-actions {
    display: flex;
    gap: 0.5rem;
  }

  .rd-save {
    flex: 1;
    padding: 0.6rem 1.2rem;
    border: none;
    border-radius: 999px;
    background: var(--cta);
    color: #fff;
    font: inherit;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--ease-lift);
  }

  .rd-save:hover:enabled {
    background: var(--cta-strong);
  }

  .rd-cancel {
    padding: 0.6rem 1rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
  }

  .rd-cancel:hover:enabled {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .rd-save:focus-visible,
  .rd-cancel:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .rd-save:disabled,
  .rd-cancel:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  @media (max-width: 599px) {
    .rd {
      align-items: flex-end;
      padding: 0;
    }

    .rd-panel {
      width: 100%;
      max-height: 85dvh;
      padding: 1.15rem max(1.2rem, env(safe-area-inset-right))
        calc(1.25rem + env(safe-area-inset-bottom)) max(1.2rem, env(safe-area-inset-left));
      border-radius: var(--radius-lg) var(--radius-lg) 0 0;
      animation: rd-rise 0.2s ease;
    }
  }

  @keyframes rd-rise {
    from {
      transform: translateY(1.5rem);
      opacity: 0.6;
    }
  }

  @media (hover: none) {
    .rd-save,
    .rd-cancel {
      min-height: 2.75rem;
    }
  }
</style>
