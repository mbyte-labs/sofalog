<script>
  // §3.9: titles by release decade, the favourite, the oldest and the newest.
  import StatSection from './StatSection.svelte'
  import StatsBarChart from './StatsBarChart.svelte'
  import BannerCard from './BannerCard.svelte'
  import { HALF_BANNER_SIZES } from '../../../lib/images.js'
  import { plural } from '../common/format.js'
  import { decadeLabel, percent } from './labels.js'

  /** @type {{ decades: import('../common/api.js').StatsDecades }} */
  let { decades } = $props()

  const short = $derived(decades.buckets.length < 10)
  const labels = $derived(decades.buckets.map((b) => decadeLabel(b.decade, short)))
  const values = $derived(decades.buckets.map((b) => b.titles))
  const favoriteTitles = $derived(
    decades.buckets.find((b) => b.decade === decades.favorite)?.titles ?? 0,
  )
</script>

<StatSection label="Decades" caption="Titles watched by release decade" plain>
  <div class="card">
    <StatsBarChart
      {labels}
      {values}
      thinLabels={decades.buckets.length > 8}
      tooltipLines={(i) => [
        `${decades.buckets[i].decade}s`,
        plural(decades.buckets[i].titles, 'title', 'titles'),
      ]}
      ariaLabel="Titles watched by release decade"
    />
    <dl class="facts">
      <div class="fact">
        <dt class="fact-label">Favorite decade</dt>
        <dd class="fact-value">
          {decades.favorite}s · {percent(
            decades.covered > 0 ? favoriteTitles / decades.covered : 0,
          )}
        </dd>
      </div>
    </dl>
    {#if decades.covered < decades.total}
      <p class="footnote">Based on {decades.covered} of {decades.total} watched titles</p>
    {/if}
  </div>

  <div class="eras" class:pair={decades.newest != null}>
    {#if decades.oldest}
      <BannerCard
        title={decades.oldest.title}
        badge="Oldest · {decades.oldest.year}"
        sizes={HALF_BANNER_SIZES}
      />
    {/if}
    {#if decades.newest}
      <BannerCard
        title={decades.newest.title}
        badge="Newest · {decades.newest.year}"
        sizes={HALF_BANNER_SIZES}
      />
    {/if}
  </div>
</StatSection>

<style>
  .facts {
    margin-top: 1rem;
  }

  dd {
    margin: 0;
  }

  .eras {
    display: grid;
    gap: 0.75rem;
    margin-top: 0.75rem;
  }

  @media (min-width: 768px) {
    .eras.pair {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
