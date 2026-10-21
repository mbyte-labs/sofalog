<script>
  // §3.3: the 1–10 distribution and the per-kind averages.
  import StatSection from './StatSection.svelte'
  import StatsBarChart from './StatsBarChart.svelte'
  import { ratingColor } from '../common/ratingColor.js'

  /** @type {{ ratings: import('../common/api.js').StatsRatings }} */
  let { ratings } = $props()

  const labels = Array.from({ length: 10 }, (_, i) => String(i + 1))
  const colors = labels.map((l) => ratingColor(Number(l)))

  /** @param {number | null} v */
  const avg = (v) => (v == null ? '—' : v.toFixed(1))
  const facts = $derived(
    [
      { label: 'Ratings given', value: String(ratings.count) },
      { label: 'Average rating', value: avg(ratings.average) },
      { label: 'Movies average', value: avg(ratings.movies_average), when: ratings.movies_average },
      { label: 'Shows average', value: avg(ratings.shows_average), when: ratings.shows_average },
      {
        label: 'Episodes average',
        value: avg(ratings.episodes_average),
        when: ratings.episodes_average,
      },
    ].filter((f) => !('when' in f) || f.when != null),
  )
</script>

<StatSection label="Your ratings">
  <StatsBarChart
    {labels}
    values={ratings.distribution}
    {colors}
    tooltipLines={(i) => [`${ratings.distribution[i]} rated ${i + 1}`]}
    ariaLabel="Ratings given, 1 to 10"
  />
  <dl class="facts">
    {#each facts as f (f.label)}
      <div class="fact">
        <dt class="fact-label">{f.label}</dt>
        <dd class="fact-value">{f.value}</dd>
      </div>
    {/each}
  </dl>
</StatSection>

<style>
  .facts {
    margin-top: 1rem;
  }

  dd {
    margin: 0;
  }
</style>
