<script>
  // One small, bounded choice. This component names the choice, collects one
  // answer, hands it to the parent and stays open while `busy` so a failure
  // keeps its context — the parent owns the request (WatchLogDialog's
  // contract). It never asks for free text; anything with a field is that
  // dialog's job, not this one's.
  //
  // `actions` is what makes one dialog serve every shape. A CONFIRMATION is
  // one entry: "Add DVD copy?" / "Remove Jellyfin copy?" on the movie and
  // season pages. A FORK is two: the show page's partly covered copy kind can
  // gain the missing seasons or lose them all, and the choice belongs to the
  // user rather than to a guess about which they meant; the up-next card's tap
  // offers marking the episode watched or opening its details page. An INFO
  // panel is an entry that only navigates: copies are recorded per season, so
  // the episode page has nothing to confirm — its one action is a link to the
  // season page and the dismiss button says "Close" rather than "Cancel".
  //
  // An entry with an `href` renders as a link wearing the same pill — an
  // anchor rather than a scripted jump because the app has no programmatic
  // navigation: every route change in the SPA is a link with the router
  // action. The focus trap already selects `[href]`. `secondary` outlines an
  // entry instead of filling it, so a fork reads as a choice rather than as
  // two equal commands; the caller says which, because only the caller knows
  // which of its actions is the lesser one.
  //
  // The modal mechanics are WatchLogDialog's, copied rather than shared — no
  // reusable modal exists in the app: scrim-as-button, Escape, a focus trap,
  // focus in on open and back to the opener on close, and a scroll lock that
  // restores the previous value. No text field here, so the panel always takes
  // focus itself.

  import { route as routeAction } from '@mateothegreat/svelte5-router'

  /**
   * @type {{
   *   heading: string,
   *   subject: string,
   *   actions: { id: string, label: string, href?: string, secondary?: boolean }[],
   *   note?: string | null,
   *   busy?: boolean,
   *   error?: string | null,
   *   cancelLabel?: string,
   *   onconfirm?: (actionId: string) => void,
   *   oncancel: () => void,
   * }}
   */
  let {
    heading,
    subject,
    actions,
    note = null,
    busy = false,
    error = null,
    cancelLabel = 'Cancel',
    onconfirm,
    oncancel,
  } = $props()

  /** @type {HTMLDivElement | undefined} */
  let panelEl = $state()
  /** @type {HTMLElement | null} whatever opened us, refocused on unmount */
  const opener = /** @type {HTMLElement | null} */ (document.activeElement)

  // The panel takes focus on open so Escape reaches the handler without
  // tabbing first — there is no field to focus instead.
  $effect(() => {
    panelEl?.focus()
  })

  // Returning focus to the control that opened the dialog is what makes a
  // keyboard round trip survive it.
  $effect(() => {
    return () => opener?.focus()
  })

  // The page behind a full-screen overlay must not scroll. Restoring the
  // previous value rather than clearing it keeps this safe if anything else
  // ever locks scrolling too.
  $effect(() => {
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previous
    }
  })

  /** @param {string} id */
  function confirm(id) {
    if (busy) return
    onconfirm?.(id)
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

  // aria-modal is a promise to the screen reader that the rest of the page is
  // unreachable; without a trap, Tab breaks that promise on the last control.
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

<div class="cd" role="dialog" aria-modal="true" aria-labelledby="cd-heading">
  <!-- Click-outside cancels. A button rather than a div so it is
       keyboard-reachable and needs no role juggling. -->
  <button type="button" class="cd-scrim" aria-label={cancelLabel} onclick={cancel}></button>

  <!-- The keydown is the focus trap, not an interaction of the panel itself —
       the same shape WatchLogDialog carries on its form. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="cd-panel" bind:this={panelEl} tabindex="-1" onkeydown={onPanelKey}>
    <h2 id="cd-heading">{heading}</h2>
    <p class="cd-subject">{subject}</p>

    {#if note}
      <p class="cd-note">{note}</p>
    {/if}
    {#if error}
      <p class="cd-error" role="alert">{error}</p>
    {/if}

    <div class="cd-actions">
      {#each actions as action (action.id)}
        {#if action.href}
          <!-- A navigation, not a write: a real link, so it keeps middle-click
               and "open in new tab" and needs no scripted jump. -->
          <a
            class="cd-confirm"
            class:secondary={action.secondary}
            href={action.href}
            use:routeAction>{action.label}</a
          >
        {:else}
          <button
            type="button"
            class="cd-confirm"
            class:secondary={action.secondary}
            disabled={busy}
            onclick={() => confirm(action.id)}
          >
            {busy ? 'Saving…' : action.label}
          </button>
        {/if}
      {/each}
      <button type="button" class="cd-cancel" onclick={cancel} disabled={busy}>
        {cancelLabel}
      </button>
    </div>
  </div>
</div>

<style>
  /* One rung above the gallery lightbox's 50 (the ladder is documented in
     App.svelte: topbar 10 < backdrop 20 < drawer 30 < lightbox 50 < this). */
  .cd {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }

  /* A full-bleed button: it is the click-outside target, so it must cover the
     viewport and sit under the panel. */
  .cd-scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: rgb(0 0 0 / 0.6);
    backdrop-filter: blur(4px);
    cursor: default;
  }

  .cd-panel {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: min(24rem, 100%);
    max-height: calc(100dvh - 2rem);
    overflow-y: auto;
    /* When the panel scrolls internally, reaching its end must not hand the
       gesture to the (locked, but rubber-banding on iOS) page behind it. */
    overscroll-behavior: contain;
    padding: 1.15rem 1.2rem 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lift);
  }

  /* The panel is focused programmatically; it needs no ring of its own. */
  .cd-panel:focus {
    outline: none;
  }

  h2 {
    margin: 0;
    font-size: 1.05rem;
  }

  /* The heading names the action, this names the thing — so it sits tight
     under it rather than reading as a paragraph of its own. */
  .cd-subject {
    margin: -0.85rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  .cd-note {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.75rem;
    font-style: italic;
  }

  .cd-error {
    margin: 0;
    color: var(--err);
    font-size: 0.8rem;
  }

  /* Stacked rather than in a row: the show page can offer two full-sentence
     choices ("Add to all 5 seasons"), and a row would squeeze them. */
  .cd-actions {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  /* The same --cta pill the pages' Mark As Watched wears. */
  .cd-confirm {
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

  /* :not(:disabled) rather than :enabled — :enabled matches only form
     elements, so the anchor variant would otherwise have no hover state. */
  .cd-confirm:hover:not(:disabled) {
    background: var(--cta-strong);
  }

  /* The anchor wears the button's pill, so it needs the box behaviours a
     button gets for free: centred label, no underline. */
  a.cd-confirm {
    display: flex;
    align-items: center;
    justify-content: center;
    text-decoration: none;
  }

  /* The caller marks the lesser of two side-by-side actions, which is then
     outlined so the pair reads as a choice, not two equal commands. */
  .cd-confirm.secondary {
    padding: calc(0.6rem - 1.5px) 1.2rem;
    border: 1.5px solid var(--cta);
    background: transparent;
    color: var(--cta);
  }

  .cd-confirm.secondary:hover:not(:disabled) {
    background: var(--accent-tint);
  }

  .cd-cancel {
    padding: 0.6rem 1rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
  }

  .cd-cancel:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .cd-confirm:focus-visible,
  .cd-cancel:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .cd-confirm:disabled,
  .cd-cancel:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  /* ── Phone: the card becomes a bottom sheet ───────────────────────────── */

  /* Anchored to the bottom edge and full width, so the confirm sits under the
     thumb. Safe-area padding follows App.svelte's idiom: max() for the sides,
     calc() for the bottom. */
  @media (max-width: 599px) {
    .cd {
      align-items: flex-end;
      padding: 0;
    }

    .cd-panel {
      width: 100%;
      max-height: 85dvh;
      padding: 1.15rem max(1.2rem, env(safe-area-inset-right))
        calc(1.25rem + env(safe-area-inset-bottom)) max(1.2rem, env(safe-area-inset-left));
      border-radius: var(--radius-lg) var(--radius-lg) 0 0;
      animation: cd-rise 0.2s ease;
    }
  }

  /* Entrance only — closing unmounts, and app.css's reduced-motion rule
     already neutralises this for users who opt out. */
  @keyframes cd-rise {
    from {
      transform: translateY(1.5rem);
      opacity: 0.6;
    }
  }

  /* ── Touch ergonomics ─────────────────────────────────────────────────── */

  /* 44px targets (the .menu-button size) where no hover pointer exists. */
  @media (hover: none) {
    .cd-confirm,
    .cd-cancel {
      min-height: 2.75rem;
    }
  }
</style>
