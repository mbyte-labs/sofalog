<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { TMDB_PATH } from '../../../lib/brandMarks.js'

  // The three beats of the round-trip. Order carries the meaning: nothing is
  // kept until step three, so the "your own copy" claim lives only there.
  const STEPS = [
    {
      label: 'You type',
      text: 'sofalog asks TMDB on the spot. Your query goes out — nothing about you or your library does.',
    },
    {
      label: 'TMDB answers',
      text: 'Results are borrowed, not saved. Their posters wait in a temporary cache that clears itself.',
    },
    {
      label: 'You add to history',
      text: 'Only then does it become yours: metadata and artwork are downloaded to this server, and sofalog stops needing TMDB for it.',
    },
  ]
</script>

<div class="intro">
  <!-- The mark's own paint server. Deliberately not #tmdb-grad — the details
       page owns that id, and two live <defs> must not collide. -->
  <svg width="0" height="0" class="svg-defs" aria-hidden="true">
    <defs>
      <linearGradient id="tmdb-grad-intro" x1="0" y1="0" x2="1" y2="0">
        <stop offset="0" stop-color="#90cea1" />
        <stop offset="1" stop-color="#01b4e4" />
      </linearGradient>
    </defs>
  </svg>

  <!-- aria-hidden: the heading and the steps already name TMDB, so labelling
       the mark would only double-announce it. Sized in CSS, not by attribute,
       so the mobile override lands. -->
  <svg class="mark" viewBox="0 0 24 24" aria-hidden="true">
    <path d={TMDB_PATH} fill="url(#tmdb-grad-intro)" />
  </svg>

  <h2>Search is the online part</h2>

  <p class="lede">
    Everything in your library is read from this server — its own database, its own disk. Search is
    the exception: sofalog has no catalog of its own, so it asks TMDB live while you type. Nothing
    you see here is part of your library yet.
  </p>

  <ol class="steps">
    {#each STEPS as step, i (step.label)}
      <li>
        <span class="num" aria-hidden="true">{i + 1}</span>
        <h3>{step.label}</h3>
        <p>{step.text}</p>
      </li>
    {/each}
  </ol>

  <a class="more" href="/settings/data-sources" use:route>How sofalog uses TMDB →</a>
</div>

<style>
  .intro {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.85rem;
    max-width: 42rem;
    margin: 0 auto;
    padding: 1.75rem 0 3rem;
    text-align: center;
  }

  .svg-defs {
    position: absolute;
  }

  .mark {
    flex: none;
    width: 4.5rem;
    height: 4.5rem;
  }

  h2 {
    margin: 0;
    font-size: 1.3rem;
  }

  .lede {
    margin: 0;
    max-width: 34rem;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.55;
  }

  .steps {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.75rem;
    width: 100%;
    margin: 0.6rem 0 0;
    padding: 0;
    list-style: none;
    text-align: left;
  }

  .steps li {
    padding: 0.85rem 1rem 0.95rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  /* The step number, not a bullet — the list is unstyled so it reads as three
     cards rather than an ordered list of prose. */
  .num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.35rem;
    height: 1.35rem;
    background: var(--accent-tint);
    border-radius: 999px;
    color: var(--accent-soft);
    font-size: 0.72rem;
    font-weight: 700;
  }

  .steps h3 {
    margin: 0.5rem 0 0.25rem;
    font-size: 0.85rem;
  }

  .steps p {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.8rem;
    line-height: 1.45;
  }

  /* Same pill as the details page's outbound links, but this one stays in the
     app — hence use:route and no target. */
  .more {
    display: inline-flex;
    align-items: center;
    height: 2.1rem;
    margin-top: 0.35rem;
    padding: 0 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text);
    font-size: 0.85rem;
    font-weight: 600;
    text-decoration: none;
    transition:
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  .more:hover {
    background: var(--accent-tint);
    border-color: var(--accent);
  }

  @media (max-width: 767px) {
    .intro {
      gap: 0.7rem;
      padding: 1rem 0 2rem;
    }

    .mark {
      width: 3.5rem;
      height: 3.5rem;
    }

    h2 {
      font-size: 1.15rem;
    }

    .steps {
      grid-template-columns: 1fr;
    }
  }
</style>
