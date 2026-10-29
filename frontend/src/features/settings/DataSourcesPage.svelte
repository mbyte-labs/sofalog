<script>
  import { route } from '@mateothegreat/svelte5-router'
  // Static page — no API calls. It documents the providers that actually run
  // today; Steam (games) gets its entry when that feature lands. The brand mark
  // is inline path data from the bundle: naming a provider here must not mean
  // fetching anything from it.
  import { JELLYFIN_PATH, TMDB_PATH } from '../../lib/brandMarks.js'
</script>

<section>
  <div class="page-head">
    <h1>Data Sources</h1>
  </div>

  <p class="intro">
    sofalog keeps its own copy of everything it fetches. Browsing your library never contacts a
    third-party service — metadata and artwork are read from this server's own database and disk.
    Outside services are only contacted when you search for something to add, browse what is
    trending or popular, open a person's page, or when the app refreshes air dates in the
    background.
  </p>

  <article class="source">
    <!-- The mark's own paint server. Deliberately not #tmdb-grad or
         #tmdb-grad-intro — the details page and the search intro own those ids,
         and two live <defs> must not collide. -->
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="tmdb-grad-sources" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stop-color="#90cea1" />
          <stop offset="1" stop-color="#01b4e4" />
        </linearGradient>
      </defs>
    </svg>

    <header>
      <!-- The wordmark is the heading: nothing repeats the name visually, and
           no separate themoviedb.org link is needed. The name still has to be
           in the outline, so it is real (hidden) text and the mark is
           decoration — an aria-label on the <svg> alone leaves the heading
           empty as far as the a11y tree is concerned. -->
      <h2>
        <a
          class="brand-link"
          href="https://www.themoviedb.org"
          target="_blank"
          rel="noreferrer noopener"
          title="themoviedb.org"
        >
          <span class="sr-only">TMDB</span>
          <svg class="mark" viewBox="0 0 24 24" aria-hidden="true">
            <path d={TMDB_PATH} fill="url(#tmdb-grad-sources)" />
          </svg>
        </a>
      </h2>
    </header>

    <p class="used-for">Used for movies and TV shows.</p>

    <h3>What it provides</h3>
    <ul>
      <li>Search results when you look for a movie or a show to track.</li>
      <li>
        The Trending page &mdash; TMDB's trending list, and its Discover list with the filters and
        sort orders TMDB offers, fetched live each time you open the page and never saved. Which of
        those titles you already track is worked out here, from your own library. Filtering by
        streaming service is the one request that tells TMDB which country this server is set to.
      </li>
      <li>Titles, release dates and first-air dates.</li>
      <li>Plot overviews.</li>
      <li>
        The TMDB community score and popularity ranking — these are TMDB's numbers, not yours.
      </li>
      <li>Poster images, in two sizes.</li>
      <li>
        The &ldquo;Watch now&rdquo; listings on a movie or show page — which services stream, rent
        or sell it in the country this server is configured for. TMDB sources these from JustWatch,
        which is why the section credits them.
      </li>
    </ul>

    <h3>How your data is handled</h3>
    <ul>
      <li>
        Posters are downloaded once and stored on this server. Your browser loads them from here and
        never talks to TMDB directly.
      </li>
      <li>
        The TMDB API key lives on the server only. It is never sent to the browser and never appears
        in the app's pages.
      </li>
      <li>
        Nothing about you is sent to TMDB — no account, no library, no watch history. TMDB only sees
        this server asking for search results and images.
      </li>
    </ul>

    <p class="notice">This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
  </article>

  <!-- Not a metadata source: the household's own server, read to mark what
       it holds (D-033). Listed here because this page is where every outside
       connection is disclosed, even one to your own hardware. -->
  <article class="source">
    <svg width="0" height="0" class="svg-defs" aria-hidden="true">
      <defs>
        <linearGradient id="jellyfin-grad-sources" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stop-color="#aa5cc3" />
          <stop offset="1" stop-color="#00a4dc" />
        </linearGradient>
      </defs>
    </svg>

    <header>
      <h2>
        <a
          class="brand-link"
          href="https://jellyfin.org"
          target="_blank"
          rel="noreferrer noopener"
          title="jellyfin.org"
        >
          <span class="sr-only">Jellyfin</span>
          <svg class="mark" viewBox="0 0 24 24" aria-hidden="true">
            <path d={JELLYFIN_PATH} fill="url(#jellyfin-grad-sources)" />
          </svg>
        </a>
      </h2>
    </header>

    <p class="used-for">
      Your own media server, if one is configured. Used for movies and TV shows.
    </p>

    <h3>What it provides</h3>
    <ul>
      <li>
        Which movies and which seasons are on the server, so the Jellyfin tile under &ldquo;Watch
        now&rdquo; lights up by itself. Items are matched by the TMDB id Jellyfin's metadata
        carries.
      </li>
    </ul>

    <h3>How your data is handled</h3>
    <ul>
      <li>
        The server is read on a schedule and when someone presses Scan now under
        <a href="/settings/jellyfin" use:route>Settings &rarr; Jellyfin</a>; never while a page
        loads.
      </li>
      <li>
        Only the library's contents are read — never what anyone has played, and nothing is written
        to the server or read from it about you.
      </li>
      <li>The API key lives on this server only and is never sent to the browser.</li>
    </ul>
  </article>

  <p class="footnote">The game data source will be listed here once that feature is built.</p>
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  .intro {
    margin: 0 0 1.5rem;
    max-width: 44rem;
    color: var(--text-dim);
    line-height: 1.5;
  }

  .source + .source {
    margin-top: 1rem;
  }

  .source {
    max-width: 44rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .svg-defs {
    position: absolute;
  }

  .source header {
    display: flex;
    align-items: center;
  }

  /* No heading text to style — the h2 exists for the page outline and gets its
     name from the mark inside it, so it only needs its box collapsed. */
  h2 {
    margin: 0;
    line-height: 0;
  }

  /* Present to the screen reader and the page outline, absent on screen. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .brand-link {
    display: inline-flex;
    opacity: 0.9;
    transition: opacity var(--ease-lift);
  }

  .brand-link:hover {
    opacity: 1;
  }

  /* Sized in CSS, not by attribute, so the mobile override lands. The stacked
     wordmark needs real height to stay readable. */
  .mark {
    width: 2.75rem;
    height: 2.75rem;
  }

  .used-for {
    margin: 0.45rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  h3 {
    margin: 1.1rem 0 0.4rem;
    color: var(--text-dim);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  ul {
    margin: 0;
    padding-left: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    line-height: 1.45;
  }

  /* The wording is mandated by TMDB's API terms — do not paraphrase it. */
  .notice {
    margin: 1.25rem 0 0;
    padding-top: 0.75rem;
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

  .footnote {
    margin: 1rem 0 0;
    max-width: 44rem;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  @media (max-width: 767px) {
    .mark {
      width: 2.25rem;
      height: 2.25rem;
    }
  }
</style>
