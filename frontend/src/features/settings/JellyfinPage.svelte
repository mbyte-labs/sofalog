<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { ApiError } from '../../lib/api/client.js'
  import { formatInstant as formatZoned } from '../../lib/time.js'
  import { getJellyfinStatus, jellyfinErrorLabel, startJellyfinScan } from './api.js'

  /** @typedef {import('./api.js').JellyfinStatus} JellyfinStatus */

  // The Jellyfin library scan (D-033): whether a server is configured, what
  // the last scan found, and a button to run one now. The scan is
  // server-wide — one household, one server — so this page is the same for
  // every account and nothing here is a per-user setting.

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'ready', status: JellyfinStatus, busy: boolean, notice: string }
   *   | { state: 'error' }}
   */
  let view = $state({ state: 'loading' })

  // Bumped on every reload so a slow response from the load before cannot
  // overwrite the newer view.
  let seq = 0

  /** @param {boolean} quiet keep the current view instead of the skeleton */
  async function load(quiet = false) {
    const id = ++seq
    if (!quiet) view = { state: 'loading' }
    try {
      const status = await getJellyfinStatus()
      if (id === seq) view = { state: 'ready', status, busy: false, notice: '' }
    } catch {
      if (id === seq && !quiet) view = { state: 'error' }
    }
  }

  $effect(() => {
    load()
  })

  // Only this boolean drives the polling effect — reading `view.status`
  // inside it would re-run (and reset) the interval on every snapshot.
  const polling = $derived.by(() => view.state === 'ready' && view.status.scanning)

  $effect(() => {
    if (!polling) return
    const timer = setInterval(() => load(true), 2000)
    return () => clearInterval(timer)
  })

  async function scanNow() {
    if (view.state !== 'ready' || view.busy) return
    view.busy = true
    view.notice = ''
    try {
      const status = await startJellyfinScan()
      if (view.state === 'ready') view = { state: 'ready', status, busy: false, notice: '' }
      // The 202 snapshot may predate the goroutine picking the kick up; the
      // poll catches up either way.
      load(true)
    } catch (err) {
      if (view.state !== 'ready') return
      view.busy = false
      if (err instanceof ApiError && err.code === 'conflict') {
        view.notice = 'A scan is already running.'
        load(true)
      } else {
        view.notice = 'Could not start the scan. Try again.'
      }
    }
  }

  /** @param {string} ts */
  function formatInstant(ts) {
    return formatZoned(ts) || ts
  }

  /** @param {number} seconds */
  function describeInterval(seconds) {
    if (seconds === 0) return 'Only when you press Scan now.'
    const hours = seconds / 3600
    if (hours >= 24 && hours % 24 === 0) {
      const days = hours / 24
      return `Every ${days === 1 ? 'day' : `${days} days`}.`
    }
    return `Every ${hours === 1 ? 'hour' : `${hours} hours`}.`
  }
</script>

<section>
  <div class="page-head">
    <h1>Jellyfin</h1>
  </div>

  {#if view.state === 'loading'}
    <div class="skeleton" aria-hidden="true"></div>
  {:else if view.state === 'error'}
    <p class="status error">Could not reach the server.</p>
    <button type="button" class="ghost" onclick={() => load()}>Retry</button>
  {:else}
    {@const status = view.status}
    <p class="intro">
      When a <a href="/settings/data-sources" use:route>Jellyfin</a> server is configured, sofalog scans
      its library and lights the Jellyfin tile under &ldquo;Watch now&rdquo; on every movie and season
      it holds — for everyone, since the server is the household's. The tile then mirrors the server:
      it can't be set by hand, and it goes dark again when the file is gone. Only what is on the server
      is read, never what has been played.
    </p>

    {#if !status.configured}
      <div class="card">
        <p class="lead">No Jellyfin server is configured.</p>
        <p class="hint">
          Set <code>JELLYFIN_URL</code> and <code>JELLYFIN_API_KEY</code> on the server (an API key from
          Jellyfin's dashboard) and restart it. Until then the Jellyfin tile is a label you set yourself.
        </p>
      </div>
    {:else}
      <div class="card">
        <dl>
          <div>
            <dt>Server</dt>
            <dd>{status.server_name ?? 'Not reached yet'}</dd>
          </div>
          <div>
            <dt>Schedule</dt>
            <dd>{describeInterval(status.interval_seconds)}</dd>
          </div>
          <div>
            <dt>Last scan</dt>
            <dd>
              {#if status.scanning}
                <span class="live">Scanning…</span>
              {:else if status.last}
                {formatInstant(status.last.finished_at)}
              {:else}
                <span class="dim">None since the server started</span>
              {/if}
            </dd>
          </div>
        </dl>

        {#if status.last}
          {@const last = status.last}
          {#if last.error}
            <p class="status error" role="alert">
              The last scan failed: {jellyfinErrorLabel(last.error)} Nothing was changed.
            </p>
          {:else}
            <dl class="counters">
              <div>
                <dt>Movies</dt>
                <dd>
                  {last.matched_movies}
                  <span class="sub"
                    >of {last.jellyfin_movies} on the server are in your library</span
                  >
                </dd>
              </div>
              <div>
                <dt>Seasons</dt>
                <dd>
                  {last.matched_seasons}
                  <span class="sub">
                    from {last.jellyfin_series} series and {last.jellyfin_episodes}
                    episode {last.jellyfin_episodes === 1 ? 'file' : 'files'}
                  </span>
                </dd>
              </div>
            </dl>
          {/if}
        {/if}

        <div class="actions">
          <button
            type="button"
            class="cta"
            onclick={scanNow}
            disabled={view.busy || status.scanning}
          >
            {status.scanning ? 'Scanning…' : 'Scan now'}
          </button>
          <button type="button" class="ghost" onclick={() => load(true)}>Refresh</button>
        </div>

        {#if view.notice}
          <p class="status error" role="alert">{view.notice}</p>
        {/if}
      </div>

      <p class="footnote">
        Movies are matched by TMDB id, so a file Jellyfin never identified stays dark. A season
        lights up once any of its episodes is on the server. A movie or show not yet in your library
        is not added by the scan — track it here first.
      </p>
    {/if}
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  .intro,
  .footnote {
    margin: 0 0 1rem;
    max-width: 44rem;
    color: var(--text-dim);
    line-height: 1.5;
  }

  .footnote {
    margin: 1rem 0 0;
    font-size: 0.85rem;
  }

  .intro a {
    color: var(--accent-soft);
  }

  code {
    font-size: 0.85em;
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

  .card {
    max-width: 44rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .lead {
    margin: 0;
    font-weight: 600;
  }

  .hint {
    margin: 0.35rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.45;
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

  .live {
    color: var(--accent);
    font-weight: 600;
  }

  .dim {
    color: var(--text-dim);
  }

  .counters {
    grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
    margin-top: 1.25rem;
  }

  .counters dd {
    font-size: 1.35rem;
    font-weight: 600;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
  }

  .sub {
    display: block;
    color: var(--text-dim);
    font-size: 0.75rem;
    font-weight: 400;
    line-height: 1.4;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    margin-top: 1.25rem;
  }

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

  @media (max-width: 599px) {
    .actions > button {
      width: 100%;
    }
  }

  @media (hover: none) {
    button {
      min-height: 2.75rem;
    }
  }
</style>
