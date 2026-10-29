<script>
  // The person page (D-027): what a cast card links to. Always live from
  // TMDB — a person has no catalog row, no snapshot and no per-user state, so
  // there is nothing to track, rate or watchlist here; the page is discovery.
  // The one thing of the user's it shows is the library marks on the
  // filmography, which the server joins locally.

  import { route as routeAction } from '@mateothegreat/svelte5-router'
  import { TMDB_PATH, IMDB_PATH } from '../../../lib/brandMarks.js'
  import { formatDay, ageAt } from '../common/format.js'
  import BrandIcon from '../common/components/BrandIcon.svelte'
  import CreditCard from '../common/components/CreditCard.svelte'
  import DetailHero from '../common/components/DetailHero.svelte'
  import '../common/detail-page.css'
  import { getPersonDetails } from '../common/api.js'
  import { ApiError } from '../../../lib/api/client.js'

  // The router remounts this component on every navigation to it, so reading
  // the query once per mount is enough.
  /** @type {{ route: any }} */
  let { route } = $props()

  // svelte-ignore state_referenced_locally
  const qs =
    route?.result?.querystring?.params ??
    Object.fromEntries(new URLSearchParams(window.location.search))
  const tmdbId = qs.tmdb_id

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'missing' }
   *   | { state: 'error', code: string }
   *   | { state: 'loaded', person: import('../common/api.js').PersonDetails }}
   */
  let view = $state({ state: 'loading' })

  if (tmdbId == null) {
    view = { state: 'missing' }
  } else {
    getPersonDetails({ tmdbId })
      .then((person) => {
        view = { state: 'loaded', person }
      })
      .catch((err) => {
        view = { state: 'error', code: err instanceof ApiError ? err.code : 'unreachable' }
      })
  }

  // ── Biography clamp ──────────────────────────────────────────────────
  // TMDB biographies run to several paragraphs; the hero shows a few lines
  // and offers the rest. The button appears only when there IS a rest —
  // measured, not guessed from a character count.

  /** @type {HTMLElement | undefined} */
  let bioEl = $state()
  let bioExpanded = $state(false)
  let bioOverflows = $state(false)

  $effect(() => {
    if (!bioEl || bioExpanded) return
    bioOverflows = bioEl.scrollHeight > bioEl.clientHeight + 1
  })

  // ── Filmography filter ───────────────────────────────────────────────
  // The list holds movies and shows side by side; the filter is presentation,
  // so it stays on the client — the server sends every credit.

  /** @type {'all' | 'movie' | 'show'} */
  let kind = $state('all')

  // $derived.by, not $derived: the callback's scope resets narrowing, so
  // `view` keeps its declared union here instead of collapsing to its
  // initial value (the Watchlist page's note).
  /** @type {import('../common/api.js').PersonCredit[]} */
  const credits = $derived.by(() => (view.state === 'loaded' ? view.person.credits : []))
  // The "Seen with" grid: the same list, same order, narrowed to what this
  // user's log holds — a movie they watched, a show they watched any episode
  // of. CreditCard renders it unchanged: a finished show wears the ✓, a
  // started one "In library", exactly as in the filmography below.
  const seen = $derived(credits.filter((c) => c.in_history))
  const movieCount = $derived(credits.filter((c) => c.media_type === 'movie').length)
  const showCount = $derived(credits.length - movieCount)
  const visible = $derived(kind === 'all' ? credits : credits.filter((c) => c.media_type === kind))

  /** @param {import('../common/api.js').PersonDetails} person */
  function lifeLine(person) {
    if (!person.birthday) return null
    const age = ageAt(person.birthday, person.deathday)
    const born = formatDay(person.birthday)
    if (person.deathday) {
      return `${born} – ${formatDay(person.deathday)}${age != null ? ` (aged ${age})` : ''}`
    }
    return `Born ${born}${age != null ? ` (age ${age})` : ''}`
  }
</script>

<section class="detail">
  {#if view.state === 'loading'}
    <div class="page-head">
      <h1>Person</h1>
    </div>
    <p class="status">Loading…</p>
  {:else if view.state === 'missing' || (view.state === 'error' && view.code === 'not_found')}
    <div class="page-head">
      <h1>Person</h1>
    </div>
    <p class="status">
      This person could not be found.
      <a href="/video/search" use:routeAction>Find a title via Search</a>.
    </p>
  {:else if view.state === 'error'}
    <div class="page-head">
      <h1>Person</h1>
    </div>
    {#if view.code === 'provider_unavailable'}
      <p class="status error">
        TMDB is unreachable right now, and a person's page is always fetched live — try again in a
        moment. Your library is unaffected.
      </p>
    {:else}
      <p class="status error">Loading failed ({view.code}).</p>
    {/if}
  {:else}
    {@const person = view.person}
    {@const life = lifeLine(person)}
    <!-- TMDB's official gradient, referenced as fill="url(#tmdb-grad)". -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <!-- A portrait is 2:3 like a poster, so the hero's default art slot fits;
         photo_full is TMDB's h632 profile variant. -->
    <DetailHero title={person.name} poster={person.photo} posterFull={person.photo_full}>
      {#snippet info()}
        <h1>{person.name}</h1>
        {#if person.known_for_department || life || person.place_of_birth}
          <p class="meta-line">
            {#if person.known_for_department}
              <span>{person.known_for_department}</span>
            {/if}
            {#if life}
              <span>{life}</span>
            {/if}
            {#if person.place_of_birth}
              <span>{person.place_of_birth}</span>
            {/if}
          </p>
        {/if}
      {/snippet}

      {#snippet desc()}
        {#if person.biography}
          <p
            class="overview bio"
            class:clamped={!bioExpanded}
            bind:this={bioEl}
            id="person-biography"
          >
            {person.biography}
          </p>
          {#if bioOverflows || bioExpanded}
            <button
              class="bio-toggle"
              onclick={() => (bioExpanded = !bioExpanded)}
              aria-expanded={bioExpanded}
              aria-controls="person-biography"
            >
              {bioExpanded ? 'Show less' : 'Read more'}
            </button>
          {/if}
        {:else}
          <p class="overview pending">TMDB has no biography for this person.</p>
        {/if}
      {/snippet}
    </DetailHero>

    <div class="detail-body">
      <div class="content">
        <!-- Outbound links (the one place the app points away from itself)
             and the facts TMDB knows. -->
        <div class="section s-details">
          <h2 class="section-label">Details</h2>
          <div class="details">
            <div class="ext-links">
              <a
                class="ext-link"
                href="https://www.themoviedb.org/person/{person.tmdb_id}"
                target="_blank"
                rel="noopener noreferrer"
              >
                <BrandIcon path={TMDB_PATH} size={18} fill="url(#tmdb-grad)" />
                TMDB
              </a>
              {#if person.imdb_id}
                <a
                  class="ext-link"
                  href="https://www.imdb.com/name/{person.imdb_id}/"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <span class="imdb-logo">
                    <BrandIcon path={IMDB_PATH} size={18} />
                  </span>
                  IMDb
                </a>
              {/if}
              {#if person.homepage}
                <a
                  class="ext-link"
                  href={person.homepage}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                    <circle
                      cx="12"
                      cy="12"
                      r="9"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.8"
                    />
                    <path
                      d="M3 12h18M12 3a13.5 13.5 0 0 1 0 18M12 3a13.5 13.5 0 0 0 0 18"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="1.8"
                    />
                  </svg>
                  Website
                </a>
              {/if}
            </div>

            {#if person.birthday || person.deathday || person.also_known_as.length > 0}
              <div class="facts">
                {#if person.birthday}
                  <div class="fact">
                    <span class="fact-label">Born</span>
                    <span class="fact-value">{formatDay(person.birthday)}</span>
                    {#if person.place_of_birth}
                      <span class="fact-note">{person.place_of_birth}</span>
                    {/if}
                  </div>
                {/if}
                {#if person.deathday}
                  <div class="fact">
                    <span class="fact-label">Died</span>
                    <span class="fact-value">{formatDay(person.deathday)}</span>
                  </div>
                {/if}
                {#if person.also_known_as.length > 0}
                  <div class="fact">
                    <span class="fact-label">Also known as</span>
                    <span class="fact-value aka">{person.also_known_as.slice(0, 4).join(', ')}</span
                    >
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>

        <!-- What this user has seen the person in — the filmography narrowed
             to their own history, so the answer to "where do I know them
             from" comes before the whole list. Stays visible when empty: the
             page says so rather than hiding the question. -->
        <div class="section s-seen">
          <h2 class="section-label">Seen with {person.name}</h2>
          {#if seen.length > 0}
            <div class="grid">
              {#each seen as credit (`${credit.media_type}:${credit.tmdb_id}`)}
                <CreditCard {credit} />
              {/each}
            </div>
          {:else}
            <p class="row-note pending">You haven't watched anything with {person.name} yet.</p>
          {/if}
        </div>

        <!-- Acting credits, one card per title, marked against the library:
             an "In library" chip or a ✓, and a link by local id when the
             catalog holds the title. -->
        <div class="section s-credits">
          <div class="credits-head">
            <h2 class="section-label">Filmography</h2>
            {#if movieCount > 0 && showCount > 0}
              <div class="segmented" role="group" aria-label="Filter credits">
                <button aria-pressed={kind === 'all'} onclick={() => (kind = 'all')}>
                  All <span class="count">{credits.length}</span>
                </button>
                <button aria-pressed={kind === 'movie'} onclick={() => (kind = 'movie')}>
                  Movies <span class="count">{movieCount}</span>
                </button>
                <button aria-pressed={kind === 'show'} onclick={() => (kind = 'show')}>
                  Shows <span class="count">{showCount}</span>
                </button>
              </div>
            {/if}
          </div>
          {#if visible.length > 0}
            <div class="grid">
              <!-- Keyed on the pair, never the id alone: movie 5 and show 5
                   both exist (D-013), and a combined list holds both kinds. -->
              {#each visible as credit (`${credit.media_type}:${credit.tmdb_id}`)}
                <CreditCard {credit} />
              {/each}
            </div>
          {:else}
            <p class="row-note pending">TMDB lists no acting credits for this person.</p>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  /* The biography is the page's own snippet markup, so its clamp is scoped
     here rather than in the shared stylesheet: no other page has one. TMDB
     separates paragraphs with blank lines, kept only once expanded: clamped,
     a blank line would be one of the six and the ellipsis would sit on it. */
  .bio {
    white-space: pre-line;
  }

  .bio.clamped {
    white-space: normal;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 6;
    line-clamp: 6;
    overflow: hidden;
  }

  .bio-toggle {
    margin-top: 0.4rem;
    padding: 0;
    background: none;
    border: none;
    color: var(--accent);
    font: inherit;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    text-shadow: 0 1px 3px rgb(0 0 0 / 0.5);
  }

  .bio-toggle:hover,
  .bio-toggle:focus-visible {
    text-decoration: underline;
  }

  .aka {
    white-space: normal;
  }

  .credits-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 0.6rem;
  }

  .credits-head :global(.section-label) {
    margin-bottom: 0;
  }

  /* The kind filter: one pressed segment, the Watchlist page's control
     tokens (surface, border, accent when active). */
  .segmented {
    display: inline-flex;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .segmented button {
    padding: 0.35rem 0.8rem;
    background: none;
    border: none;
    color: var(--text-dim);
    font: inherit;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
  }

  .segmented button + button {
    border-left: 1px solid var(--border);
  }

  .segmented button:hover {
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

  .count {
    margin-left: 0.25rem;
    opacity: 0.75;
    font-weight: 400;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--poster-min), 1fr));
    gap: 1.25rem 1rem;
  }

  @media (hover: none) {
    .segmented button,
    .bio-toggle {
      min-height: 2.75rem;
    }
  }
</style>
