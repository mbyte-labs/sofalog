<script>
  // Chooser between backup formats. Static — the work happens on the per-format
  // page it links to. The Ticket Box mark is a flat SVG in public/icons, served
  // from this app like the logo: naming the source app must not mean fetching
  // anything from it.
  import { route } from '@mateothegreat/svelte5-router'
</script>

<section>
  <div class="page-head">
    <h1>Restore backup</h1>
  </div>

  <p class="intro">
    Bring your watch history, ratings, watchlist and favorites back from a file. Pick the app the
    backup came from.
  </p>

  <ul class="sources">
    <li>
      <a href="/settings/restore/ticket-box" use:route class="card">
        <img class="mark" src="./icons/ticket-box.svg" alt="" />
        <span class="text">
          <span class="title">Ticket Box dump</span>
          <span class="blurb">
            A backup exported from the Ticket Box app: watch history, ratings, watchlist and
            favorites.
          </span>
        </span>
      </a>
    </li>
    <li>
      <!-- Not a link: there is nothing behind it yet. aria-disabled, rather
           than a disabled button, because it is not an action either — it is a
           placeholder telling the reader what will be here. -->
      <div class="card disabled" aria-disabled="true">
        <img class="mark" src="./icons/logo-mark.svg" alt="" />
        <span class="text">
          <span class="title">
            Sofalog dump
            <span class="badge">Coming later</span>
          </span>
          <span class="blurb">Restore a sofalog export archive.</span>
        </span>
      </div>
    </li>
  </ul>
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

  .sources {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
    max-width: 40rem;
  }

  /* Same card treatment as the Settings section links. */
  .card {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    padding: 0.75rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-decoration: none;
    transition:
      border-color var(--ease-lift),
      transform var(--ease-lift);
  }

  a.card:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }

  .card.disabled {
    opacity: 0.55;
    cursor: default;
  }

  .mark {
    width: 2.75rem;
    height: 2.75rem;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }

  .title {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.5rem;
    color: var(--text);
    font-weight: 600;
  }

  .badge {
    padding: 0.1rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-dim);
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .blurb {
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

  @media (hover: none) {
    a.card:hover {
      transform: none;
    }
  }
</style>
