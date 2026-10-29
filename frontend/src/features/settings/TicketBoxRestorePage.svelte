<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { ApiError } from '../../lib/api/client.js'
  import { formatInstant } from '../../lib/time.js'
  import {
    getTicketBoxRestore,
    startTicketBoxRestore,
    readableMessage,
    skipReasonLabel,
    skipReasonExplanation,
  } from './api.js'

  /** @typedef {import('./api.js').RestoreJob} RestoreJob */
  /** @typedef {import('./api.js').RestoreSkip} RestoreSkip */

  /**
   * The parsed file, as far as the client looks at it. Everything else in it
   * is the server's to validate — the body goes up verbatim.
   * @typedef {{ schemaVersion: 5, exportedAt?: unknown, history?: unknown, ratings?: unknown, watchlist?: unknown, favorites?: unknown }} Dump
   */

  // Files larger than this are refused before being read: the server caps the
  // body at the same size, and a real dump is a few hundred KB at most.
  const MAX_BYTES = 32 * 1024 * 1024

  /**
   * One view at a time. `probing` is the mount-time read of the last job;
   * `pick` waits for a file (notice: why the last one was refused); `ready`
   * shows the parsed file and the two-step confirm; `starting` is the POST in
   * flight; `job` shows a running or finished import (`lost` marks a failed
   * poll, the snapshot kept); `error` is an unreachable server on the probe.
   *
   * @type {{ state: 'probing' }
   *   | { state: 'pick', notice: string }
   *   | { state: 'ready', dump: Dump, fileName: string, notice: string, conflict: boolean, confirming: boolean }
   *   | { state: 'starting', dump: Dump, fileName: string }
   *   | { state: 'job', job: RestoreJob, lost: boolean }
   *   | { state: 'error' }}
   */
  let view = $state({ state: 'probing' })

  // Bumped whenever the page moves to something new (a file is picked, an
  // import starts, the user asks for another file), so a response from the
  // phase before — a slow probe, a poll already in flight — cannot overwrite
  // the newer view.
  let seq = 0

  async function probe() {
    const id = ++seq
    view = { state: 'probing' }
    try {
      const job = await getTicketBoxRestore()
      if (id === seq) view = { state: 'job', job, lost: false }
    } catch (err) {
      if (id !== seq) return
      if (err instanceof ApiError && err.code === 'not_found') view = { state: 'pick', notice: '' }
      else view = { state: 'error' }
    }
  }

  $effect(() => {
    probe()
  })

  // Only this boolean drives the polling effect — reading `view.job` inside
  // it would re-run (and reset) the interval on every snapshot.
  // $derived.by, not $derived: the callback's scope resets narrowing, so
  // `view` keeps its declared union here instead of collapsing to its initial
  // value.
  const polling = $derived.by(() => view.state === 'job' && view.job.state === 'running')

  $effect(() => {
    if (!polling) return
    const id = seq
    const timer = setInterval(async () => {
      try {
        const job = await getTicketBoxRestore()
        if (id === seq) view = { state: 'job', job, lost: false }
      } catch (err) {
        if (id !== seq) return
        if (err instanceof ApiError && err.code === 'not_found') {
          // The job lives in server memory: a restart between two polls
          // forgets it. Re-running is safe — the import replaces per item.
          view = {
            state: 'pick',
            notice:
              'The server restarted and lost track of the import. Pick the file again to re-run it — that is safe, the import replaces what it already wrote.',
          }
        } else if (view.state === 'job') {
          view.lost = true
        }
      }
    }, 2000)
    return () => clearInterval(timer)
  })

  /** @param {Event & { currentTarget: HTMLInputElement }} e */
  async function onPick(e) {
    const input = e.currentTarget
    const file = input.files?.[0]
    // Reset so picking the same file again (after a fix) fires change.
    input.value = ''
    if (!file) return
    const id = ++seq

    /** @param {string} notice */
    const refuse = (notice) => {
      if (id === seq) view = { state: 'pick', notice }
    }

    if (file.size > MAX_BYTES) return refuse('That file is too large (32 MB limit).')

    let text
    try {
      text = await file.text()
    } catch {
      return refuse("That file couldn't be read.")
    }

    let parsed
    try {
      parsed = JSON.parse(text)
    } catch {
      return refuse("That isn't a JSON file.")
    }

    const isObject = parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)
    if (!isObject || parsed.schemaVersion !== 5) {
      const v = isObject ? parsed.schemaVersion : undefined
      return refuse(
        `This file is Ticket Box schema version ${v ?? 'unknown'}; sofalog imports version 5 only. Update Ticket Box and export again.`,
      )
    }

    if (id === seq)
      view = {
        state: 'ready',
        dump: parsed,
        fileName: file.name,
        notice: '',
        conflict: false,
        confirming: false,
      }
  }

  async function submit() {
    if (view.state !== 'ready') return
    const { dump, fileName } = view
    const id = ++seq
    view = { state: 'starting', dump, fileName }
    try {
      const job = await startTicketBoxRestore(dump)
      if (id === seq) view = { state: 'job', job, lost: false }
    } catch (err) {
      if (id !== seq) return
      let notice = 'Could not start the import. Try again.'
      let conflict = false
      if (err instanceof ApiError) {
        if (err.code === 'conflict') {
          notice = 'An import is already running.'
          conflict = true
        } else if (err.code === 'validation') {
          notice = readableMessage(err.message)
        }
      }
      // Back to ready, not pick: the file is fine to keep around.
      view = { state: 'ready', dump, fileName, notice, conflict, confirming: false }
    }
  }

  // A function, not an inline closure in the markup: narrowing does not
  // survive into a closure, so `view.confirming = …` there fails the check.
  /** @param {boolean} on */
  function setConfirming(on) {
    if (view.state === 'ready') view.confirming = on
  }

  function pickAnother() {
    ++seq
    view = { state: 'pick', notice: '' }
  }

  /** @param {unknown} value */
  const count = (value) => (Array.isArray(value) ? value.length : 0)

  /** @param {unknown} value */
  function formatExportedAt(value) {
    if (typeof value !== 'string' || !value) return 'unknown'
    return formatInstant(value) || value
  }

  /** @param {RestoreJob} job */
  function progressLine(job) {
    const { groups_done, groups_total, current } = job.progress
    if (!current) return 'Preparing…'
    const n = Math.min(groups_done + 1, groups_total)
    return `Importing ${current.type} ${n} of ${groups_total} — ${current.title ?? 'Fetching…'}`
  }

  /** @param {RestoreJob} job */
  function percent(job) {
    const { groups_done, groups_total } = job.progress
    return groups_total > 0 ? Math.round((groups_done / groups_total) * 100) : 0
  }

  /** @type {Record<string, string>} */
  const KIND_LABELS = {
    history: 'Watch',
    rating: 'Rating',
    watchlist: 'Watchlist',
    favorite: 'Favorite',
  }

  /**
   * What a skip row names: the movie or show (its title when the catalog
   * holds it), then for a season or episode entry the season, then the
   * episode. Links go by TMDB id, which every page accepts whether or not the
   * item is tracked — except when TMDB itself has no such item, where the
   * page would only say so.
   * @param {RestoreSkip} s
   */
  function skipTarget(s) {
    const linkable = s.reason !== 'movie_not_found' && s.reason !== 'show_not_found'
    if (s.type === 'movie') {
      return {
        name: s.title ?? `Movie ${s.tmdb_id ?? '?'}`,
        href: linkable && s.tmdb_id != null ? `/video/movie-details?tmdb_id=${s.tmdb_id}` : null,
        season: null,
        episode: null,
      }
    }
    // A season or episode entry's own tmdb_id is not the show's.
    const showId = s.type === 'show' ? (s.tmdb_id ?? s.show_tmdb_id) : s.show_tmdb_id
    const inSeason = (s.type === 'season' || s.type === 'episode') && s.season != null
    return {
      name: s.title ?? (showId != null ? `Show ${showId}` : s.type || 'Unknown'),
      href: linkable && showId != null ? `/video/show-details?tmdb_id=${showId}` : null,
      season: inSeason
        ? {
            label: s.season === 0 ? 'Specials' : `Season ${s.season}`,
            href:
              linkable && showId != null
                ? `/video/season-details?tmdb_id=${showId}&season=${s.season}`
                : null,
          }
        : null,
      episode: s.type === 'episode' && s.episode != null ? `Episode ${s.episode}` : null,
    }
  }

  /**
   * The finished-job counters, in display order. `tone` picks the number's
   * colour: what landed is green, what was skipped is red — only when there
   * is something to colour, a zero of either stays plain.
   * @param {RestoreJob} job
   */
  function counters(job) {
    const r = job.report
    return [
      { label: 'Watches added', value: r.history_inserted, tone: 'ok' },
      { label: 'Ratings set', value: r.ratings_upserted, tone: 'ok' },
      { label: 'Watchlist entries added', value: r.watchlist_inserted, tone: 'ok' },
      { label: 'Favorites added', value: r.favorites_inserted, tone: 'ok' },
      { label: 'Skipped', value: r.skipped_total, tone: 'err' },
      {
        label: 'Movies added to the library',
        value: r.movies_materialised,
        sub: `of ${r.movies_total} in the file`,
        tone: 'ok',
      },
      {
        label: 'Shows added to the library',
        value: r.shows_materialised,
        sub: `of ${r.shows_total} in the file`,
        tone: 'ok',
      },
    ]
  }
</script>

<section>
  <div class="page-head">
    <h1>Restore a Ticket Box dump</h1>
  </div>

  {#if view.state === 'probing'}
    <div class="skeleton" aria-hidden="true"></div>
  {:else if view.state === 'error'}
    <p class="status error">Could not reach the server.</p>
    <button type="button" class="ghost" onclick={probe}>Retry</button>
  {:else if view.state === 'pick' || view.state === 'ready' || view.state === 'starting'}
    <p class="intro">
      Watch history, ratings, watchlist and favorites from the file are imported into your library.
      For every movie or show that has watches in the file, your existing sofalog watches for it are
      replaced by the file's. Ratings, watchlist entries and favorites are added or updated. Nothing
      else is touched.
    </p>
    <p class="intro">
      Movies and shows not yet in your library are fetched from
      <a href="/settings/data-sources" use:route>TMDB</a>, so a large file takes a while.
    </p>

    {#if view.state === 'pick'}
      <!-- The input is visually hidden but still in the tab order; the label
           is what looks like the button. -->
      <label class="file-button">
        <input type="file" accept="application/json,.json" class="sr-only" onchange={onPick} />
        Choose a Ticket Box export…
      </label>
      {#if view.notice}
        <p class="status error" role="alert">{view.notice}</p>
      {/if}
    {:else}
      <div class="file-card">
        <dl>
          <div>
            <dt>File</dt>
            <dd class="file-name">{view.fileName}</dd>
          </div>
          <div>
            <dt>Exported</dt>
            <dd>{formatExportedAt(view.dump.exportedAt)}</dd>
          </div>
          <div>
            <dt>Watches</dt>
            <dd>{count(view.dump.history)}</dd>
          </div>
          <div>
            <dt>Ratings</dt>
            <dd>{count(view.dump.ratings)}</dd>
          </div>
          <div>
            <dt>Watchlist</dt>
            <dd>{count(view.dump.watchlist)}</dd>
          </div>
          <div>
            <dt>Favorites</dt>
            <dd>{count(view.dump.favorites)}</dd>
          </div>
        </dl>

        <div class="actions">
          {#if view.state === 'starting'}
            <button type="button" class="cta" disabled>Starting…</button>
          {:else if view.confirming}
            <span class="confirm-text">Replace matching history with this file?</span>
            <button type="button" class="cta" onclick={submit}>Yes, import</button>
            <button type="button" class="ghost" onclick={() => setConfirming(false)}>
              Cancel
            </button>
          {:else}
            <button type="button" class="cta" onclick={() => setConfirming(true)}> Import </button>
            <button type="button" class="ghost" onclick={pickAnother}>Pick a different file</button>
          {/if}
        </div>

        {#if view.state === 'ready' && view.notice}
          <p class="status error" role="alert">
            {view.notice}
            {#if view.conflict}
              <button type="button" class="ghost inline" onclick={probe}>Show its progress</button>
            {/if}
          </p>
        {/if}
      </div>
    {/if}
  {:else if view.job.state === 'running'}
    {@const job = view.job}
    {@const pct = percent(job)}
    <div class="job">
      <p class="live" aria-live="polite">{progressLine(job)}</p>
      <div
        class="bar"
        role="progressbar"
        aria-valuemin="0"
        aria-valuemax={job.progress.groups_total}
        aria-valuenow={job.progress.groups_done}
        aria-label="Import progress"
      >
        <div class="fill" style="width: {pct}%"></div>
      </div>
      <p class="bar-caption">
        {job.progress.groups_done} of {job.progress.groups_total} movies and shows · {pct}%
      </p>

      {#if view.lost}
        <p class="status error">Connection lost, retrying…</p>
      {/if}

      <dl class="counters live-counters">
        {#each counters(job) as c (c.label)}
          <div>
            <dt>{c.label}</dt>
            <dd>{c.value}</dd>
          </div>
        {/each}
        <div>
          <dt>Existing watches replaced</dt>
          <dd>{job.report.history_replaced}</dd>
        </div>
      </dl>
    </div>
  {:else}
    {@const job = view.job}
    {@const skipped = job.report.skipped ?? []}
    <div class="job">
      {#if job.state === 'failed'}
        <h2 class="outcome failed">Import failed</h2>
        {#if job.error}
          <p class="status error">{job.error.message}</p>
        {/if}
      {:else}
        <h2 class="outcome">Import finished</h2>
      {/if}

      {#if job.finished_at}
        <p class="finished-at">Finished {formatExportedAt(job.finished_at)}</p>
      {/if}

      <dl class="counters">
        {#each counters(job) as c (c.label)}
          <div>
            <dt>{c.label}</dt>
            <dd
              class:ok={c.tone === 'ok' && c.value > 0}
              class:err={c.tone === 'err' && c.value > 0}
            >
              {c.value}
              {#if c.sub}<span class="sub">{c.sub}</span>{/if}
            </dd>
          </div>
        {/each}
      </dl>

      <p class="replaced">
        Replaced {job.report.history_replaced} existing
        {job.report.history_replaced === 1 ? 'watch' : 'watches'}
      </p>

      <h3>Skipped</h3>
      {#if job.report.skipped_total === 0}
        <p class="nothing">Nothing was skipped.</p>
      {:else}
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Kind</th>
                <th scope="col">Item</th>
                <th scope="col">Reason</th>
              </tr>
            </thead>
            <tbody>
              {#each skipped as s, i (i)}
                {@const target = skipTarget(s)}
                {@const why = skipReasonExplanation(s)}
                <tr>
                  <td data-label="Kind">{KIND_LABELS[s.kind] ?? s.kind}</td>
                  <td data-label="Item" class="item">
                    {#if target.href}
                      <a href={target.href} use:route>{target.name}</a>
                    {:else}
                      {target.name}
                    {/if}
                    {#if target.season}
                      <span class="sep" aria-hidden="true">·</span>
                      {#if target.season.href}
                        <a href={target.season.href} use:route>{target.season.label}</a>
                      {:else}
                        {target.season.label}
                      {/if}
                    {/if}
                    {#if target.episode}
                      <span class="sep" aria-hidden="true">·</span>
                      {target.episode}
                    {/if}
                  </td>
                  <td data-label="Reason">
                    {skipReasonLabel(s.reason)}
                    {#if why}<span class="why">{why}</span>{/if}
                  </td>
                </tr>
              {/each}
            </tbody>
            {#if job.report.skipped_total > skipped.length}
              <tfoot>
                <tr>
                  <td colspan="3">and {job.report.skipped_total - skipped.length} more</td>
                </tr>
              </tfoot>
            {/if}
          </table>
        </div>
      {/if}

      <div class="actions">
        <button type="button" class="cta" onclick={pickAnother}>Import another file</button>
      </div>
    </div>
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

  /* Present to keyboard and screen reader, absent on screen. The label wrapping
     it is the visible control, so :focus-within paints the ring there. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .file-button,
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
    cursor: pointer;
    transition:
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  .file-button {
    position: relative;
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text);
  }

  .file-button:hover,
  .file-button:focus-within {
    border-color: var(--accent);
  }

  .file-button:focus-within {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  button.cta {
    background: var(--cta);
    color: #fff;
  }

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

  button.ghost.inline {
    margin-left: 0.5rem;
    padding: 0.2rem 0.55rem;
    font-size: 0.8rem;
  }

  .file-card,
  .job {
    max-width: 44rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  dl {
    margin: 0;
  }

  .file-card dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
    gap: 0.75rem 1rem;
  }

  .file-card dl > div:first-child {
    grid-column: 1 / -1;
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
    font-variant-numeric: tabular-nums;
  }

  .file-name {
    overflow-wrap: anywhere;
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

  .live {
    margin: 0 0 0.75rem;
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .bar {
    height: 0.6rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 999px;
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    border-radius: inherit;
    transition: width 0.4s ease;
  }

  .bar-caption {
    margin: 0.4rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
    font-variant-numeric: tabular-nums;
  }

  .counters {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
    gap: 0.9rem 1rem;
    margin-top: 1.25rem;
  }

  .counters dd {
    font-size: 1.35rem;
    font-weight: 600;
    line-height: 1.2;
  }

  .live-counters dd {
    font-size: 1.1rem;
  }

  .counters dd.ok {
    color: var(--ok);
  }

  .counters dd.err {
    color: var(--err);
  }

  .sub {
    display: block;
    color: var(--text-dim);
    font-size: 0.75rem;
    font-weight: 400;
  }

  .outcome {
    margin: 0;
    font-size: 1.15rem;
  }

  .outcome.failed {
    color: var(--err);
  }

  .finished-at {
    margin: 0.25rem 0 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  .replaced {
    margin: 1rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  h3 {
    margin: 1.25rem 0 0.5rem;
    color: var(--text-dim);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .nothing {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }

  th,
  td {
    padding: 0.4rem 0.6rem 0.4rem 0;
    text-align: left;
    vertical-align: top;
    border-bottom: 1px solid var(--border);
  }

  th {
    color: var(--text-dim);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .item a {
    color: var(--accent-soft);
  }

  .sep {
    margin: 0 0.25rem;
    color: var(--text-dim);
  }

  .why {
    display: block;
    margin-top: 0.15rem;
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  tfoot td {
    border-bottom: none;
    color: var(--text-dim);
    font-style: italic;
  }

  /* Phone: each row stacks into a labelled card, the header row goes away. */
  @media (max-width: 599px) {
    thead {
      display: none;
    }

    tr,
    td {
      display: block;
    }

    tr {
      padding: 0.5rem 0;
      border-bottom: 1px solid var(--border);
    }

    td {
      padding: 0.15rem 0;
      border-bottom: none;
    }

    /* Under the value, not under the row's label column. */
    .why {
      padding-left: 4.5rem;
    }

    tbody td::before {
      content: attr(data-label);
      display: inline-block;
      width: 4.5rem;
      color: var(--text-dim);
      font-size: 0.7rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }

    .actions > button,
    .file-button {
      width: 100%;
    }
  }

  @media (hover: none) {
    .file-button,
    button {
      min-height: 2.75rem;
    }

    button.ghost.inline {
      min-height: 2.75rem;
      margin-left: 0;
      margin-top: 0.5rem;
      display: flex;
    }
  }
</style>
