<script>
  // §3.7: watched movies with no rating — the most recent as a banner with
  // Rate, the next four as rows with Rate. The parent opens the dialog and
  // owns the write.
  import StatSection from './StatSection.svelte'
  import BannerCard from './BannerCard.svelte'
  import TitleRow from './TitleRow.svelte'
  import { plural, watchedAgo } from '../common/format.js'
  import { dayOf } from '../../../lib/time.js'
  import { titleName } from './labels.js'

  /**
   * @type {{
   *   unrated: import('../common/api.js').StatsUnratedMovies,
   *   today: string,
   *   onrate: (title: import('../common/api.js').StatsTitle) => void,
   * }}
   */
  let { unrated, today, onrate } = $props()

  const hero = $derived(unrated.recent[0])
  const rest = $derived(unrated.recent.slice(1))
</script>

<StatSection
  label="Waiting for your rating"
  caption={plural(
    unrated.total,
    'watched movie without a rating',
    'watched movies without a rating',
  )}
  plain
>
  {#if hero}
    <BannerCard
      title={hero.movie}
      badge="Watched {watchedAgo(dayOf(hero.last_played_at), today)}"
      subtitle={hero.movie.year ? String(hero.movie.year) : null}
    >
      {#snippet action()}
        <button
          class="btn-rate"
          aria-label="Rate {titleName(hero.movie)}"
          onclick={() => onrate(hero.movie)}
        >
          Rate
        </button>
      {/snippet}
    </BannerCard>
  {/if}
  {#if rest.length > 0}
    <ul class="row-list card">
      {#each rest as m (m.movie.id)}
        <TitleRow title={m.movie} sub={m.movie.year ? String(m.movie.year) : null}>
          {#snippet action()}
            <button
              class="btn-rate text"
              aria-label="Rate {titleName(m.movie)}"
              onclick={() => onrate(m.movie)}
            >
              Rate
            </button>
          {/snippet}
        </TitleRow>
      {/each}
    </ul>
  {/if}
</StatSection>

<style>
  .row-list.card {
    margin-top: 0.75rem;
    padding: 0.5rem 0.6rem;
  }
</style>
