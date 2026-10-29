<script>
  // §3.5: the average rating per month on a fixed 0–10 scale, and the last
  // 90 days against everything before.
  import StatSection from './StatSection.svelte'
  import StatsBarChart from './StatsBarChart.svelte'
  import { formatMonth, plural, signedDelta } from '../common/format.js'
  import { trendVerdict } from './labels.js'

  /** @type {{ trend: import('../common/api.js').StatsRatingTrend, period: string, isPastYear: boolean }} */
  let { trend, period, isPastYear } = $props()

  const labels = $derived(trend.months.map((m) => formatMonth(m.month, true)))
  const values = $derived(trend.months.map((m) => m.average ?? 0))

  /** @param {import('../common/api.js').StatsRatingSample} s */
  const sample = (s) => (s.average == null ? `— (0)` : `${s.average.toFixed(1)} avg (${s.count})`)

  /** @param {number} i */
  function lines(i) {
    const m = trend.months[i]
    return [
      formatMonth(m.month),
      m.average == null
        ? 'No ratings'
        : `${m.average.toFixed(1)} avg · ${plural(m.count, 'rating', 'ratings')}`,
    ]
  }
</script>

<StatSection label="Ratings over time" caption="Average rating per month (movies & shows)">
  <StatsBarChart
    {labels}
    {values}
    fixedMax={10}
    thinLabels
    tooltipLines={lines}
    ariaLabel="Average rating per month"
  />
  <dl class="facts">
    <div class="fact">
      <dt class="fact-label">{isPastYear ? `Last 90 days of ${period}` : 'Last 90 days'}</dt>
      <dd class="fact-value">{sample(trend.recent)}</dd>
    </div>
    <div class="fact">
      <dt class="fact-label">Before that</dt>
      <dd class="fact-value">{sample(trend.earlier)}</dd>
    </div>
    {#if trend.delta != null && trend.verdict != null}
      <div class="fact">
        <dt class="fact-label">{trendVerdict(trend.verdict)}</dt>
        <dd class="fact-value delta" class:up={trend.delta > 0} class:down={trend.delta < 0}>
          {signedDelta(trend.delta)}
        </dd>
      </div>
    {/if}
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
