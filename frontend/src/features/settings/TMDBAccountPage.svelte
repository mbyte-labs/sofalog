<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { ApiError } from '../../lib/api/client.js'
  import { formatInstant } from '../../lib/time.js'
  import {
    getTMDBLink,
    setTMDBSync,
    setupTMDBSync,
    skipReasonLabel,
    tmdbLinkResultMessage,
    tmdbLinkStartUrl,
    tmdbPullErrorMessage,
    unlinkTMDB,
  } from './api.js'

  /** @typedef {import('./api.js').TMDBLinkStatus} TMDBLinkStatus */
  /** @typedef {import('./api.js').TMDBSetupRequest} TMDBSetupRequest */
  /** @typedef {import('./api.js').TMDBSetupAction} TMDBSetupAction */
  /** @typedef {'ratings' | 'watchlist' | 'favorites'} SyncKind */

  // The callback redirects here with ?tmdb_link=<code>; it cannot render JSON
  // at a browser mid-redirect, so the outcome travels in the URL and becomes a
  // sentence here. Read once at mount — the banner belongs to this landing.
  const result = tmdbLinkResultMessage(
    new URLSearchParams(window.location.search).get('tmdb_link') ?? '',
  )

  // The three kinds the setup and the switches speak about (D-036), with the
  // one-line consequence of each one-time action.
  /** @type {{ key: SyncKind, label: string, pull: string, push: string }[]} */
  const SECTIONS = [
    {
      key: 'ratings',
      label: 'Ratings',
      pull: 'Import the ratings on your TMDB account once. A rating you already have here is replaced by TMDB’s.',
      push: 'Send the ratings you already have here to TMDB once.',
    },
    {
      key: 'watchlist',
      label: 'Watchlist',
      pull: 'Add everything on your TMDB watchlist to your watchlist here, once. Nothing here is removed.',
      push: 'Send your watchlist here to TMDB once.',
    },
    {
      key: 'favorites',
      label: 'Favorites',
      pull: 'Add your TMDB favorites to your favorites here, once. Nothing here is removed.',
      push: 'Send your favorites here to TMDB once.',
    },
  ]

  /** @returns {TMDBSetupRequest} */
  function newDraft() {
    return {
      ratings: { action: 'skip', sync: false },
      watchlist: { action: 'skip', sync: false },
      favorites: { action: 'skip', sync: false },
    }
  }

  /**
   * draft is the setup form, kept as long as the setup is not done so a
   * failed pull brings the panel back with the choices intact. syncBusy names
   * the switch being saved. lost flags a failed poll while a pull runs.
   *
   * @type {{ state: 'loading' }
   *   | { state: 'ready', status: TMDBLinkStatus, confirming: boolean, busy: boolean, notice: string,
   *       draft: TMDBSetupRequest, setupBusy: boolean, setupNotice: string, syncBusy: SyncKind | '',
   *       syncNotice: string, lost: boolean }
   *   | { state: 'error' }}
   */
  let view = $state({ state: 'loading' })

  // Bumped on every reload so a slow response from the load before cannot
  // overwrite the newer view.
  let seq = 0

  async function load() {
    const id = ++seq
    view = { state: 'loading' }
    try {
      const status = await getTMDBLink()
      if (id === seq) {
        view = {
          state: 'ready',
          status,
          confirming: false,
          busy: false,
          notice: '',
          draft: newDraft(),
          setupBusy: false,
          setupNotice: '',
          syncBusy: '',
          syncNotice: '',
          lost: false,
        }
      }
    } catch {
      if (id === seq) view = { state: 'error' }
    }
  }

  $effect(() => {
    load()
  })

  // Poll while the setup's pull runs. Only a derived boolean drives the
  // effect: reading view.status inside it would restart the interval on
  // every snapshot (the restore page's pattern).
  const polling = $derived.by(() => view.state === 'ready' && view.status.pull?.state === 'running')

  $effect(() => {
    if (!polling) return
    const id = seq
    const timer = setInterval(async () => {
      try {
        const status = await getTMDBLink()
        if (id === seq && view.state === 'ready') {
          view.status = status
          view.lost = false
        }
      } catch {
        if (id === seq && view.state === 'ready') view.lost = true
      }
    }, 2000)
    return () => clearInterval(timer)
  })

  // Functions, not inline closures in the markup: narrowing does not survive
  // into a closure, so `view.confirming = …` there fails the check.
  /** @param {boolean} on */
  function setConfirming(on) {
    if (view.state === 'ready') view.confirming = on
  }

  async function unlink() {
    if (view.state !== 'ready') return
    view.busy = true
    view.notice = ''
    try {
      await unlinkTMDB()
      await load()
    } catch {
      if (view.state === 'ready') {
        view.busy = false
        view.confirming = false
        view.notice = 'Could not unlink. Try again.'
      }
    }
  }

  /**
   * @param {SyncKind} kind
   * @param {TMDBSetupAction} action
   */
  function pickAction(kind, action) {
    if (view.state !== 'ready') return
    view.draft[kind].action = action
    // Pull and push both mean "I want these on both sides", so the switch
    // follows; skip means "leave it alone", and so does its switch.
    view.draft[kind].sync = action !== 'skip'
  }

  /**
   * @param {SyncKind} kind
   * @param {Event} e
   */
  function toggleDraftSync(kind, e) {
    if (view.state !== 'ready') return
    view.draft[kind].sync = /** @type {HTMLInputElement} */ (e.currentTarget).checked
  }

  async function submitSetup() {
    if (view.state !== 'ready') return
    view.setupBusy = true
    view.setupNotice = ''
    try {
      const status = await setupTMDBSync($state.snapshot(view.draft))
      if (view.state === 'ready') {
        view.status = status
        view.setupBusy = false
      }
    } catch (err) {
      if (view.state !== 'ready') return
      view.setupBusy = false
      if (err instanceof ApiError && err.code === 'conflict') {
        // The link broke, or the setup landed from another tab: re-read.
        await load()
        return
      }
      view.setupNotice = 'Could not save the setup. Try again.'
    }
  }

  // The switches are NOT bound: the server's answer is the source of truth,
  // replaced after the save, and a failure puts the box back where the
  // server still has it (the account page's matching-watchlist pattern).
  /**
   * @param {SyncKind} kind
   * @param {Event} e
   */
  async function toggleSync(kind, e) {
    if (view.state !== 'ready') return
    const input = /** @type {HTMLInputElement} */ (e.currentTarget)
    const on = input.checked
    view.syncBusy = kind
    view.syncNotice = ''
    try {
      const status = await setTMDBSync({ ...view.status.sync, [kind]: on })
      if (view.state === 'ready') view.status = status
    } catch {
      input.checked = !on
      if (view.state === 'ready') view.syncNotice = 'Could not change the setting. Try again.'
    } finally {
      if (view.state === 'ready') view.syncBusy = ''
    }
  }

  /** @param {string | null} ts */
  function formatLinkedAt(ts) {
    if (!ts) return ''
    return formatInstant(ts, { dateStyle: 'medium' }) || ts
  }

  /** @param {number} n */
  function pendingLine(n) {
    if (n === 0) return 'Everything has been sent.'
    return n === 1 ? '1 change is waiting to be sent.' : `${n} changes are waiting to be sent.`
  }

  /** @param {import('./api.js').TMDBPullProgress} p */
  function progressLine(p) {
    const what = p.section ? `${p.section} — ${p.list}` : 'your lists'
    const pages = p.total_pages > 0 ? `page ${p.page} of ${p.total_pages}` : `page ${p.page}`
    return `Importing ${what}, ${pages}. ${p.items_done} ${p.items_done === 1 ? 'entry' : 'entries'} so far.`
  }

  /** @param {import('./api.js').TMDBPullReport} r */
  function reportCounters(r) {
    return [
      { label: 'Ratings imported', value: r.ratings_imported },
      { label: 'Episode ratings imported', value: r.episode_ratings_imported },
      { label: 'Added to watchlist', value: r.watchlist_added },
      { label: 'Added to favorites', value: r.favorites_added },
      { label: 'Movies added to the library', value: r.movies_materialised },
      { label: 'Shows added to the library', value: r.shows_materialised },
      { label: 'Skipped', value: r.skipped_total, err: true },
    ]
  }
</script>

<section>
  <div class="page-head">
    <h1>TMDB account</h1>
  </div>

  {#if result}
    <p
      class="status banner"
      class:ok={result.tone === 'ok'}
      class:error={result.tone === 'error'}
      role="status"
    >
      {result.text}
    </p>
  {/if}

  {#if view.state === 'loading'}
    <div class="skeleton" aria-hidden="true"></div>
  {:else if view.state === 'error'}
    <p class="status error">Could not reach the server.</p>
    <button type="button" class="ghost" onclick={load}>Retry</button>
  {:else}
    {@const status = view.status}
    {@const pull = status.pull}
    {@const pulling = pull?.state === 'running'}
    <p class="intro">
      Link your own <a href="/settings/data-sources" use:route>TMDB</a> account, choose once what to do
      with the ratings, watchlist and favorites each side already holds, and from then on the kinds you
      keep in sync are sent to it — one way. After that one-time setup nothing is read back, and removing
      something here removes it there. Unlinking stops the sending and leaves your TMDB account as it
      is.
    </p>

    {#if !status.configured}
      <div class="card">
        <p class="nothing">
          This server has no TMDB credential configured, so accounts cannot be linked.
        </p>
      </div>
    {:else if !status.linked}
      <div class="card">
        <p class="lead">No TMDB account is linked.</p>
        <p class="hint">
          You will be sent to TMDB to approve the link, then brought back here to choose, for your
          ratings, watchlist and favorites, whether to import TMDB’s list, send yours, or do nothing
          — and which to keep in sync from then on.
        </p>
        <div class="actions">
          <!-- A plain anchor, no router action: this is a full navigation out
               to TMDB and back, not a route inside the app. -->
          <a class="cta" href={tmdbLinkStartUrl()}>Link TMDB account</a>
        </div>
      </div>
    {:else}
      {#if !status.setup_done && !status.broken && !pulling}
        <div class="card setup">
          <h2>Set up the sync</h2>
          <p class="hint">
            For each kind, choose what happens now with what you and TMDB already hold. Nothing is
            sent or imported until you confirm.
          </p>
          {#if pull?.state === 'failed' && pull.error}
            <p class="status error" role="alert">{tmdbPullErrorMessage(pull.error.code)}</p>
          {/if}
          {#each SECTIONS as section (section.key)}
            {@const choice = view.draft[section.key]}
            <fieldset class="section">
              <legend>{section.label}</legend>
              <div class="segmented" role="group" aria-label="{section.label} now">
                <button
                  type="button"
                  aria-pressed={choice.action === 'pull'}
                  disabled={view.setupBusy}
                  onclick={() => pickAction(section.key, 'pull')}>Pull from TMDB</button
                >
                <button
                  type="button"
                  aria-pressed={choice.action === 'push'}
                  disabled={view.setupBusy}
                  onclick={() => pickAction(section.key, 'push')}>Push to TMDB</button
                >
                <button
                  type="button"
                  aria-pressed={choice.action === 'skip'}
                  disabled={view.setupBusy}
                  onclick={() => pickAction(section.key, 'skip')}>Skip</button
                >
              </div>
              <p class="choice-hint">
                {#if choice.action === 'pull'}
                  {section.pull}
                {:else if choice.action === 'push'}
                  {section.push}
                {:else}
                  Do nothing now.
                {/if}
              </p>
              <label class="check">
                <input
                  type="checkbox"
                  checked={choice.sync}
                  disabled={view.setupBusy}
                  onchange={(e) => toggleDraftSync(section.key, e)}
                />
                <span>Keep {section.label.toLowerCase()} in sync from now on</span>
              </label>
            </fieldset>
          {/each}
          <div class="actions">
            <button type="button" class="cta" disabled={view.setupBusy} onclick={submitSetup}>
              {view.setupBusy ? 'Saving…' : 'Confirm'}
            </button>
          </div>
          {#if view.setupNotice}
            <p class="status error" role="alert">{view.setupNotice}</p>
          {/if}
        </div>
      {/if}

      {#if pull && (pulling || pull.state === 'done' || status.setup_done)}
        <div class="card">
          <h2>{pulling ? 'Importing from TMDB' : 'Last import from TMDB'}</h2>
          {#if pulling}
            <p class="live" aria-live="polite">{progressLine(pull.progress)}</p>
            <div class="bar" aria-hidden="true"><div class="fill indeterminate"></div></div>
            {#if view.lost}
              <p class="status error" role="alert">Connection lost, retrying…</p>
            {/if}
          {:else if pull.state === 'failed' && pull.error}
            <p class="status error" role="alert">{tmdbPullErrorMessage(pull.error.code)}</p>
          {/if}
          {#if !pulling || pull.progress.items_done > 0}
            <dl class="counters">
              {#each reportCounters(pull.report) as c (c.label)}
                <div>
                  <dt>{c.label}</dt>
                  <dd class:err={c.err && c.value > 0}>{c.value}</dd>
                </div>
              {/each}
            </dl>
            {#if pull.report.skipped_total > 0}
              <ul class="skips">
                {#each Object.entries(pull.report.skipped_by_reason) as [reason, n] (reason)}
                  <li>{skipReasonLabel(reason)}: {n}</li>
                {/each}
              </ul>
            {/if}
          {/if}
        </div>
      {/if}

      <div class="card">
        {#if status.broken}
          <p class="status error" role="alert">
            TMDB no longer accepts this link. Nothing is being sent — link again to resume{status.setup_done
              ? ''
              : ', then finish the setup'}.
          </p>
        {/if}
        <dl>
          <div>
            <dt>Linked as</dt>
            <dd>@{status.username}</dd>
          </div>
          <div>
            <dt>Since</dt>
            <dd>{formatLinkedAt(status.linked_at)}</dd>
          </div>
          <div>
            <dt>Queue</dt>
            <dd class="pending" class:live={status.pending > 0}>{pendingLine(status.pending)}</dd>
          </div>
        </dl>

        <div class="actions">
          {#if status.broken}
            <a class="cta" href={tmdbLinkStartUrl()}>Link again</a>
          {/if}
          {#if view.busy}
            <button type="button" class="ghost" disabled>Unlinking…</button>
          {:else if view.confirming}
            <span class="confirm-text">
              Stop sending ratings, watchlist and favorite changes to @{status.username}? Your TMDB
              account stays as it is.
            </span>
            <button type="button" class="danger" onclick={unlink}>Yes, unlink</button>
            <button type="button" class="ghost" onclick={() => setConfirming(false)}>Cancel</button>
          {:else}
            <button type="button" class="ghost" onclick={() => setConfirming(true)}>Unlink</button>
            {#if status.pending > 0}
              <button type="button" class="ghost" onclick={load}>Refresh</button>
            {/if}
          {/if}
        </div>

        {#if view.notice}
          <p class="status error" role="alert">{view.notice}</p>
        {/if}
      </div>

      {#if status.setup_done}
        <div class="card">
          <h2>Keep in sync</h2>
          <p class="hint">
            From now on, changes you make here are sent to TMDB for the kinds you tick. Turning one
            off stops the sending and forgets what was still queued; turning it on does not resend
            what you already have.
          </p>
          <div class="switches">
            {#each SECTIONS as section (section.key)}
              <label class="check">
                <input
                  type="checkbox"
                  checked={status.sync[section.key]}
                  disabled={view.syncBusy !== ''}
                  onchange={(e) => toggleSync(section.key, e)}
                />
                <span>{section.label}</span>
              </label>
            {/each}
          </div>
          {#if view.syncNotice}
            <p class="status error" role="alert">{view.syncNotice}</p>
          {/if}
        </div>
      {/if}
    {/if}
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  .intro {
    margin: 0 0 1rem;
    max-width: 44rem;
    color: var(--text-dim);
    line-height: 1.5;
  }

  .intro a {
    color: var(--accent-soft);
  }

  .skeleton {
    max-width: 44rem;
    height: 6rem;
    background: var(--surface);
    border-radius: var(--radius-md);
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }

  .status {
    margin: 0.75rem 0 0;
    font-size: 0.9rem;
    line-height: 1.4;
  }

  .status.error {
    color: var(--err);
  }

  .status.ok {
    color: var(--ok);
  }

  .banner {
    max-width: 44rem;
    margin: 0 0 1rem;
    padding: 0.6rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .card {
    max-width: 44rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .card + .card {
    margin-top: 1rem;
  }

  .card .status.error:first-child {
    margin: 0 0 0.9rem;
  }

  .card h2 {
    margin: 0 0 0.35rem;
    font-size: 1rem;
    font-weight: 600;
  }

  .lead {
    margin: 0;
    font-weight: 600;
  }

  .hint,
  .nothing {
    margin: 0.35rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.45;
  }

  .nothing {
    margin: 0;
  }

  dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 0.75rem 1rem;
    margin: 0;
  }

  dt {
    color: var(--text-dim);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  dd {
    margin: 0.1rem 0 0;
    overflow-wrap: anywhere;
  }

  .pending {
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  .pending.live {
    color: var(--text);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    margin-top: 1.25rem;
  }

  .confirm-text {
    flex-basis: 100%;
    color: var(--text);
    font-size: 0.9rem;
  }

  a.cta,
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    padding: 0.55rem 1rem;
    border: none;
    border-radius: var(--radius-md);
    font-size: 0.95rem;
    font-weight: 600;
    text-decoration: none;
    cursor: pointer;
    transition:
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  a.cta,
  button.cta {
    background: var(--cta);
    color: #fff;
  }

  a.cta:hover,
  button.cta:hover:not(:disabled) {
    background: var(--cta-strong);
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  button.ghost {
    padding: 0.5rem 0.8rem;
    background: none;
    border: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.85rem;
    font-weight: 500;
  }

  button.ghost:hover:not(:disabled) {
    background: rgb(255 255 255 / 0.05);
    color: var(--text);
  }

  button.danger {
    background: var(--err);
    color: #fff;
  }

  /* The setup panel: one fieldset per kind. */
  .section {
    margin: 1rem 0 0;
    padding: 0.85rem 0 0;
    border: none;
    border-top: 1px solid var(--border);
  }

  .section legend {
    padding: 0;
    float: left;
    width: 100%;
    margin-bottom: 0.5rem;
    font-weight: 600;
  }

  /* The Trending page's segmented control: one pressed segment. */
  .segmented {
    display: inline-flex;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .segmented button {
    padding: 0.4rem 0.85rem;
    background: none;
    border: none;
    border-radius: 0;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.85rem;
    font-weight: 600;
  }

  .segmented button + button {
    border-left: 1px solid var(--border);
  }

  .segmented button:hover:not(:disabled) {
    color: var(--text);
  }

  .segmented button[aria-pressed='true'] {
    background: var(--accent);
    color: #fff;
  }

  .segmented button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .choice-hint {
    margin: 0.5rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

  /* The account page's native-checkbox row. */
  .check {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-top: 0.6rem;
    color: var(--text);
    font-size: 0.95rem;
    cursor: pointer;
  }

  .check input {
    margin: 0;
    padding: 0;
    width: 1.15rem;
    height: 1.15rem;
    border: none;
    background: none;
    accent-color: var(--accent);
    cursor: pointer;
  }

  .check input:disabled {
    cursor: default;
  }

  .switches {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem 1.5rem;
    margin-top: 0.5rem;
  }

  /* The restore page's progress bar, indeterminate: the total is per list,
     not per run. */
  .live {
    margin: 0.5rem 0 0;
    color: var(--accent);
    font-size: 0.9rem;
    font-weight: 600;
  }

  .bar {
    height: 0.6rem;
    margin-top: 0.6rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 999px;
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    border-radius: inherit;
  }

  .fill.indeterminate {
    width: 35%;
    animation: slide 1.4s ease-in-out infinite;
  }

  @keyframes slide {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(300%);
    }
  }

  .counters {
    grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
    gap: 0.9rem 1rem;
    margin-top: 1rem;
  }

  .counters dd {
    font-size: 1.35rem;
    font-weight: 600;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
  }

  .counters dd.err {
    color: var(--err);
  }

  .skips {
    margin: 0.75rem 0 0;
    padding-left: 1.1rem;
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.5;
  }

  @media (max-width: 599px) {
    .actions > a.cta,
    .actions > button {
      width: 100%;
    }

    .segmented {
      display: flex;
    }

    .segmented button {
      flex: 1;
      padding-inline: 0.4rem;
    }
  }

  @media (hover: none) {
    a.cta,
    button,
    .check {
      min-height: 2.75rem;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .fill.indeterminate {
      animation: none;
      width: 100%;
      opacity: 0.5;
    }
  }
</style>
