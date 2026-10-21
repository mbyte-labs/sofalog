<script>
  // §3.1: the four headline tiles and the movies-vs-TV split beneath them.
  import StatSection from './StatSection.svelte'
  import { compactNumber } from '../common/format.js'
  import { splitPercent } from './labels.js'

  /** @type {{ headline: import('../common/api.js').StatsHeadline, label: string }} */
  let { headline, label } = $props()

  const total = $derived(headline.movie_minutes + headline.episode_minutes)
  const tvShare = $derived(total > 0 ? headline.episode_minutes / total : null)
  const tiles = $derived([
    { label: 'Hours watched', value: compactNumber(headline.hours) },
    { label: 'Total plays', value: compactNumber(headline.plays) },
    { label: 'Movies', value: compactNumber(headline.movies) },
    { label: 'Episodes', value: compactNumber(headline.episodes) },
  ])
</script>

<StatSection {label} plain>
  <div class="tiles">
    {#each tiles as t (t.label)}
      <div class="tile">
        <span class="tile-value">{t.value}</span>
        <span class="tile-label">{t.label}</span>
      </div>
    {/each}
  </div>

  {#if tvShare != null}
    <div class="split card">
      <p class="split-head">
        {#if tvShare >= 0.5}
          {splitPercent(headline.episode_minutes, total)} of your time is TV
        {:else}
          {splitPercent(headline.movie_minutes, total)} of your time is movies
        {/if}
      </p>
      <div class="split-bar" aria-hidden="true">
        <span class="tv" style="width: {(headline.episode_minutes / total) * 100}%"></span>
        <span class="movies" style="width: {(headline.movie_minutes / total) * 100}%"></span>
      </div>
      <p class="split-legend">
        <span
          ><i class="swatch tv"></i>TV · {compactNumber(
            Math.floor(headline.episode_minutes / 60),
          )}h</span
        >
        <span
          ><i class="swatch movies"></i>Movies · {compactNumber(
            Math.floor(headline.movie_minutes / 60),
          )}h</span
        >
      </p>
    </div>
  {/if}
</StatSection>

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.75rem;
  }

  .tile {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    padding: 1rem 1.15rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .tile-value {
    font-size: 1.8rem;
    font-weight: 700;
    line-height: 1.1;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
  }

  .tile-label {
    color: var(--text-dim);
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .split {
    margin-top: 0.75rem;
  }

  .split-head {
    margin: 0 0 0.6rem;
    font-weight: 600;
  }

  .split-bar {
    display: flex;
    height: 0.6rem;
    overflow: hidden;
    border-radius: 999px;
    background: var(--bg);
    border: 1px solid var(--border);
  }

  .split-bar span {
    display: block;
    height: 100%;
  }

  .tv,
  .swatch.tv {
    background: var(--accent);
  }

  .movies,
  .swatch.movies {
    background: var(--cta);
  }

  .split-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem 1.25rem;
    margin: 0.6rem 0 0;
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  .swatch {
    display: inline-block;
    width: 0.6rem;
    height: 0.6rem;
    margin-right: 0.4rem;
    border-radius: 2px;
    vertical-align: middle;
  }

  @media (min-width: 1100px) {
    .tiles {
      grid-template-columns: repeat(4, 1fr);
    }
  }
</style>
