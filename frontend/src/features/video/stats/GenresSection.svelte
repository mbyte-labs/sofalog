<script>
  // §3.12: the top genres over the period's watched titles, movies and shows.
  import StatSection from './StatSection.svelte'

  /** @type {{ genres: import('../common/api.js').StatsGenres, period: string }} */
  let { genres, period } = $props()

  const top = $derived(genres.entries[0]?.titles ?? 1)
</script>

<StatSection label="Top genres">
  {#if genres.entries.length > 0}
    <ol class="genres">
      {#each genres.entries as g (g.name)}
        <li class="genre">
          <span class="genre-name">{g.name}</span>
          <span class="meter" aria-hidden="true">
            <span class="meter-fill" style="width: {Math.round((g.titles / top) * 100)}%"></span>
          </span>
          <span class="genre-count">{g.titles}</span>
        </li>
      {/each}
    </ol>
  {:else}
    <p class="empty">No genres known for these titles yet.</p>
  {/if}
  {#if genres.covered < genres.total}
    <p class="footnote">
      Based on {genres.covered} of {genres.total}
      {period === 'all' ? 'watched titles' : `titles watched in ${period}`}
    </p>
  {/if}
</StatSection>

<style>
  .genres {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .genre {
    display: grid;
    grid-template-columns: minmax(6rem, 1fr) 3fr auto;
    align-items: center;
    gap: 0.75rem;
  }

  .genre .meter {
    display: block;
  }

  .genre .meter-fill {
    display: block;
  }

  .genre-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .genre-count {
    min-width: 2rem;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--text-dim);
  }

  .empty {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  @media (max-width: 599px) {
    .genre {
      grid-template-columns: 1fr auto;
    }

    .genre .meter {
      grid-column: 1 / -1;
    }
  }
</style>
