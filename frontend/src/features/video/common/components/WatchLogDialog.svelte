<script>
  import { localDayOf } from '../format.js'
  import { wallTimeToInstant, zonedParts } from '../../../../lib/time.js'
  // "When did you watch it?" — the modal the movie, episode, show and season
  // pages open instead of writing a watch straight away. In its single mode
  // (movie/episode) confirming always adds one history entry, which is what
  // makes a rewatch loggable (D-009). In its bulk mode (the show page's
  // whole-show mark and the season page's whole-season one) it also asks the
  // two bulk questions: remaining episodes or a full rewatch, and where each
  // row's date comes from. Every mark the SPA writes passes through here; the
  // season grid's stills are read-only badges.
  //
  // The parent owns the request: this component collects the answers, hands
  // them over, and stays open while `busy` so a failure does not lose what
  // was typed.
  //
  // No reusable modal exists in the app, so the mechanics here are the
  // lightbox's from ./MediaGallery.svelte — scrim-as-button, Escape, focus in
  // on open and back to the opener on close, scroll lock that restores the
  // previous value — plus a focus trap, which a form dialog needs and an image
  // viewer did not.

  import RatingBar from './RatingBar.svelte'

  /**
   * What bulk mode reports back: the scope axis, the date axis, and the one
   * instant the server needs from the client — the fixed date, or season_air's
   * fallback for seasons without a premiere.
   * @typedef {{ rewatch: boolean,
   *   mode: 'now'|'fixed'|'episode_air'|'season_air',
   *   watchedAt: string | null }} BulkChoice
   */

  /**
   * @type {{
   *   title: string,
   *   busy?: boolean,
   *   error?: string | null,
   *   initialRating?: number | null,
   *   note?: string | null,
   *   onconfirm?: (watch: { watchedAt: string, rating: number | null }) => void,
   *   bulk?: {
   *     kind?: 'show' | 'season',
   *     watched: number,
   *     aired: number,
   *     seasonsNeedingDate: number,
   *     onconfirm: (choice: BulkChoice) => void,
   *   } | null,
   *   oncancel: () => void,
   * }}
   */
  let {
    // What is being logged, shown under the heading: the dialog is opened from
    // one page, but it should still name the thing it is about.
    title,
    // A write is in flight: the buttons go inert and Save says so.
    busy = false,
    // The parent's failure notice. Shown in the same slot as this component's
    // own validation message, because only one can be true at a time.
    error = null,
    // The rating the page currently shows, so the bar opens on it rather than
    // empty. Reported back only if the user actually moves it.
    initialRating = null,
    // An advisory the parent wants read before Save — the episode page's
    // "the first mark downloads the whole show" warning.
    note = null,
    // Single-mode confirm. Unused (and unrequired) when `bulk` is set.
    onconfirm,
    // Bulk mode: the show page's whole-show mark, or the season page's
    // whole-season one (`kind`, defaulting to 'show', only flavors the copy).
    // `watched`/`aired` are the page's derived counts for the marked scope (0
    // when nothing is known), and drive the scope radios; `seasonsNeedingDate`
    // is how many of the scope's non-special seasons have no premiere the
    // server could date season_air rows from — 0 or 1 for a season page —
    // which is what makes the fallback date field appear. Null keeps the
    // single mode untouched.
    bulk = null,
    oncancel,
  } = $props()

  // ── Bulk-mode answers ─────────────────────────────────────────────────
  // Read once on mount like everything else here. Remaining-only is the
  // default; a fully-watched show has nothing remaining, so there the rewatch
  // is the only sensible answer and becomes the default.
  // svelte-ignore state_referenced_locally
  const remaining = bulk ? Math.max(0, bulk.aired - bulk.watched) : 0
  // svelte-ignore state_referenced_locally
  let rewatch = $state(bulk != null && bulk.watched > 0 && remaining === 0)
  /** @type {'now'|'fixed'|'episode_air'|'season_air'} */
  let dateMode = $state('now')

  // Whether the date+time pair is on screen — always in single mode, and in
  // bulk mode only when the server needs an instant from the client: the
  // fixed mode's shared one, or season_air's fallback. $derived.by, not
  // $derived: a bare expression is evaluated where dateMode is still narrowed
  // to its initial value; a callback sees the declared union.
  const needsDate = $derived.by(
    () =>
      bulk == null ||
      dateMode === 'fixed' ||
      (dateMode === 'season_air' && bulk.seasonsNeedingDate > 0),
  )

  /** @param {number} n */
  const pad = (n) => String(n).padStart(2, '0')

  // Prefilled with "now", the overwhelmingly common answer, and read once: the
  // parent mounts this component on open, so there is nothing to react to.
  // Both fields are wall-clock time in the user's zone (D-038), not the
  // device's — the zone the history renders the result in.
  const opened = new Date()
  const openedAt = zonedParts(opened)
  let day = $state(localDayOf(opened))
  let clock = $state(`${pad(openedAt.hour)}:${pad(openedAt.minute)}`)
  // Reading the prop's initial value is the point: the bar opens on what the
  // page currently shows and is the user's to move from there.
  // svelte-ignore state_referenced_locally
  let rating = $state(initialRating)

  // A watch cannot be in the future. The native max blocks the picker; the
  // check in confirm() is what actually enforces it, since the field can also
  // be typed into.
  const maxDay = localDayOf(opened)

  /** @type {string | null} this dialog's own validation message */
  let problem = $state(null)

  /** @type {HTMLInputElement | undefined} */
  let dayEl = $state()
  /** @type {HTMLFormElement | undefined} */
  let panelEl = $state()
  /** @type {HTMLElement | null} whatever opened us, refocused on unmount */
  const opener = /** @type {HTMLElement | null} */ (document.activeElement)

  // Focus the first field on open, so the date is editable immediately and
  // Escape reaches the handler without tabbing first. Only where a hover
  // pointer exists, though: on touch an auto-activated field pops the
  // platform's picker over a dialog the user has not read yet, so there the
  // panel itself takes focus — Escape and the trap still work, nothing opens.
  // Bulk mode opens with no date field, so the panel takes focus there too.
  $effect(() => {
    if (matchMedia('(hover: hover)').matches && dayEl != null) dayEl.focus()
    else panelEl?.focus()
  })

  // Returning focus to the button that opened the dialog is what makes a
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

  /**
   * The instant the two fields describe as wall-clock time in the user's zone
   * (D-038) — never `new Date(y, m, d, hh, mm)`, which would read them in the
   * device's zone instead.
   *
   * @returns {Date | null} null when either field is empty or nonsense
   */
  function instant() {
    const [hh, mm] = clock.split(':').map(Number)
    return wallTimeToInstant(day, hh, mm)
  }

  /** @param {SubmitEvent} event */
  function confirm(event) {
    event.preventDefault()
    if (busy) return
    // Only the fields on screen are validated: a bulk mark dated "now" or
    // from air dates has no instant of its own to check.
    /** @type {string | null} */
    let watchedAt = null
    if (needsDate) {
      const at = instant()
      if (at == null) {
        problem = 'Enter both a date and a time.'
        return
      }
      if (at.getTime() > Date.now()) {
        problem = 'That is in the future — pick a moment that has already happened.'
        return
      }
      watchedAt = at.toISOString()
    }
    problem = null
    if (bulk) {
      bulk.onconfirm({ rewatch, mode: dateMode, watchedAt })
      return
    }
    onconfirm?.({
      watchedAt: /** @type {string} */ (watchedAt),
      // Only a rating the user actually moved travels: leaving the bar alone
      // must not rewrite the rating row, and clearing it here is not a delete
      // — the page's own rating bar owns that.
      rating: rating != null && rating !== initialRating ? rating : null,
    })
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

<div class="wl" role="dialog" aria-modal="true" aria-labelledby="wl-heading">
  <!-- Click-outside cancels. A button rather than a div so it is
       keyboard-reachable and needs no role juggling. -->
  <button type="button" class="wl-scrim" aria-label="Cancel" onclick={cancel}></button>

  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <form
    class="wl-panel"
    bind:this={panelEl}
    tabindex="-1"
    onsubmit={confirm}
    onkeydown={onPanelKey}
  >
    <h2 id="wl-heading">
      {bulk
        ? bulk.kind === 'season'
          ? 'Mark season as watched'
          : 'Mark show as watched'
        : 'Log a watch'}
    </h2>
    <p class="wl-subject">{title}</p>

    {#if bulk && bulk.watched > 0}
      <!-- Only a show with progress has two answers here; a fresh one gets the
           remaining-only mark without being asked. -->
      <div class="wl-group" role="radiogroup" aria-label="Which episodes">
        <span class="wl-field-label">Which episodes</span>
        <label class="wl-radio">
          <input
            type="radio"
            name="wl-scope"
            value={false}
            bind:group={rewatch}
            disabled={busy || remaining === 0}
          />
          <span>
            Mark the {remaining} remaining {remaining === 1 ? 'episode' : 'episodes'}
          </span>
        </label>
        <label class="wl-radio">
          <input type="radio" name="wl-scope" value={true} bind:group={rewatch} disabled={busy} />
          <span>
            Log all {bulk.aired > 0 ? bulk.aired : ''} episodes again — a full rewatch
          </span>
        </label>
      </div>
    {/if}

    {#if bulk}
      <div class="wl-group" role="radiogroup" aria-label="Watched when">
        <span class="wl-field-label">Watched when</span>
        <label class="wl-radio">
          <input
            type="radio"
            name="wl-date-mode"
            value="now"
            bind:group={dateMode}
            disabled={busy}
          />
          <span>Now</span>
        </label>
        <label class="wl-radio">
          <input
            type="radio"
            name="wl-date-mode"
            value="fixed"
            bind:group={dateMode}
            disabled={busy}
          />
          <span>A specific date &amp; time</span>
        </label>
        <label class="wl-radio">
          <input
            type="radio"
            name="wl-date-mode"
            value="episode_air"
            bind:group={dateMode}
            disabled={busy}
          />
          <span>Each episode's release date</span>
        </label>
        <label class="wl-radio">
          <input
            type="radio"
            name="wl-date-mode"
            value="season_air"
            bind:group={dateMode}
            disabled={busy}
          />
          <span>
            {bulk.kind === 'season' ? "The season's premiere date" : "Each season's premiere date"}
          </span>
        </label>
      </div>
    {/if}

    {#if needsDate}
      {#if bulk && dateMode === 'season_air'}
        <!-- Only reachable when seasonsNeedingDate > 0 (see needsDate): the
             seasons that do have a premiere keep it; this dates the rest. -->
        <p class="wl-fallback-note">
          {bulk.kind === 'season'
            ? 'This season has no premiere date — its episodes get this date instead.'
            : bulk.seasonsNeedingDate === 1
              ? '1 season has no premiere date — its episodes get this date instead.'
              : `${bulk.seasonsNeedingDate} seasons have no premiere date — their episodes get this date instead.`}
        </p>
      {/if}
      <div class="wl-when">
        <label>
          <span>Date</span>
          <input type="date" bind:this={dayEl} bind:value={day} max={maxDay} required />
        </label>
        <label>
          <span>Time</span>
          <input type="time" bind:value={clock} required />
        </label>
      </div>
    {/if}

    {#if !bulk}
      <div class="wl-rating">
        <span class="wl-field-label">Your rating <span class="wl-optional">optional</span></span>
        <!-- No `onrate`: here the bar is a plain picker, and the value is saved
             with the watch rather than on every tap. The bulk endpoint takes no
             rating — the show page has its own bar. -->
        <RatingBar bind:value={rating} disabled={busy} />
      </div>
    {/if}

    {#if note}
      <p class="wl-note">{note}</p>
    {/if}
    {#if problem ?? error}
      <p class="wl-error" role="alert">{problem ?? error}</p>
    {/if}

    <div class="wl-actions">
      <button type="submit" class="wl-save" disabled={busy}>
        {busy ? 'Saving…' : 'Save'}
      </button>
      <button type="button" class="wl-cancel" onclick={cancel} disabled={busy}>Cancel</button>
    </div>
  </form>
</div>

<style>
  /* One rung above the gallery lightbox's 50 (the ladder is documented in
     App.svelte: topbar 10 < backdrop 20 < drawer 30 < lightbox 50 < this). */
  .wl {
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
  .wl-scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: rgb(0 0 0 / 0.6);
    backdrop-filter: blur(4px);
    cursor: default;
  }

  .wl-panel {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: min(26rem, 100%);
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

  /* The panel is focused programmatically on touch devices (instead of the
     date field, whose picker would pop open); it needs no ring of its own. */
  .wl-panel:focus {
    outline: none;
  }

  h2 {
    margin: 0;
    font-size: 1.05rem;
  }

  /* The heading names the action, this names the thing — so it sits tight
     under it rather than reading as a paragraph of its own. */
  .wl-subject {
    margin: -0.85rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  /* Date and time are one answer, so they share a row and split it evenly;
     under ~22rem of panel the two fields stack instead of squeezing. */
  .wl-when {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }

  .wl-when label {
    flex: 1 1 8rem;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  /* The app has no shared input styling — this mirrors the auth pages' scoped
     block so the two read as one system. */
  input {
    padding: 0.5rem 0.6rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    /* Not a taste call: anything under 16px makes iOS Safari zoom the page
       when the field is focused. */
    font-size: 1rem;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  /* A titled radio group — the bulk questions. Same label idiom as the date
     fields, so the two read as one form. */
  .wl-group {
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }

  /* Rows, not the generic label's column: the circle and its text are one
     answer and read side by side. */
  .wl-radio {
    flex-direction: row;
    align-items: center;
    gap: 0.55rem;
    color: var(--text);
    cursor: pointer;
  }

  .wl-radio:has(input:disabled) {
    color: var(--text-dim);
    cursor: not-allowed;
  }

  /* The generic input styling below is for text-shaped fields; a radio keeps
     its native circle, just recoloured. */
  .wl-radio input {
    width: 1.05rem;
    height: 1.05rem;
    margin: 0;
    padding: 0;
    background: none;
    border: none;
    accent-color: var(--accent);
    flex: none;
  }

  /* Why the date pair is asking again, when season premieres were the point. */
  .wl-fallback-note {
    margin: 0 0 -0.5rem;
    color: var(--text-dim);
    font-size: 0.75rem;
  }

  .wl-rating {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .wl-field-label {
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  .wl-optional {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    opacity: 0.7;
  }

  .wl-note {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.75rem;
    font-style: italic;
  }

  .wl-error {
    margin: 0;
    color: var(--err);
    font-size: 0.8rem;
  }

  .wl-actions {
    display: flex;
    gap: 0.5rem;
  }

  /* The same --cta pill the pages' Mark As Watched wears: this is now where
     that write is actually confirmed. */
  .wl-save {
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

  .wl-save:hover:enabled {
    background: var(--cta-strong);
  }

  .wl-cancel {
    padding: 0.6rem 1rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
  }

  .wl-cancel:hover:enabled {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .wl-save:focus-visible,
  .wl-cancel:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .wl-save:disabled,
  .wl-cancel:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  /* ── Phone: the card becomes a bottom sheet ───────────────────────────── */

  /* Anchored to the bottom edge and full width, so Save sits under the thumb.
     Capped short of the viewport so the scrim stays visible above it — the
     panel already scrolls internally when it must. Safe-area padding follows
     App.svelte's idiom: max() for the sides, calc() for the bottom. */
  @media (max-width: 599px) {
    .wl {
      align-items: flex-end;
      padding: 0;
    }

    .wl-panel {
      width: 100%;
      max-height: 85dvh;
      padding: 1.15rem max(1.2rem, env(safe-area-inset-right))
        calc(1.25rem + env(safe-area-inset-bottom)) max(1.2rem, env(safe-area-inset-left));
      border-radius: var(--radius-lg) var(--radius-lg) 0 0;
      animation: wl-rise 0.2s ease;
    }
  }

  /* Entrance only — closing unmounts, and app.css's reduced-motion rule
     already neutralises this for users who opt out. */
  @keyframes wl-rise {
    from {
      transform: translateY(1.5rem);
      opacity: 0.6;
    }
  }

  /* ── Touch ergonomics ─────────────────────────────────────────────────── */

  /* 44px targets (the .menu-button size) where no hover pointer exists.
     RatingBar deepens its own segments on touch, so no override needed. A
     radio keeps its native circle — its whole label row is the target. */
  @media (hover: none) {
    input:not([type='radio']),
    .wl-save,
    .wl-cancel {
      min-height: 2.75rem;
    }

    .wl-radio {
      min-height: 2.75rem;
    }
  }
</style>
