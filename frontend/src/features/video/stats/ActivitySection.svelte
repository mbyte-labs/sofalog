<script>
  // §3.8: plays per month, the daily heatmap, weekdays, time of day, the
  // habit facts and the first play.
  import StatSection from './StatSection.svelte'
  import StatsBarChart from './StatsBarChart.svelte'
  import PlaysHeatmap from './PlaysHeatmap.svelte'
  import TitleRow from './TitleRow.svelte'
  import { formatDay, formatMonth, plural } from '../common/format.js'
  import { dayOf } from '../../../lib/time.js'
  import { PARTS, WEEKDAYS_LONG, WEEKDAYS_SHORT } from './labels.js'

  /**
   * @type {{
   *   activity: import('../common/api.js').StatsActivity,
   *   period: string,
   *   isPastYear: boolean,
   * }}
   */
  let { activity, period, isPastYear } = $props()

  const isYear = $derived(period !== 'all')
  const monthLabels = $derived(activity.months.map((m) => formatMonth(m.month, true)))
  const monthValues = $derived(activity.months.map((m) => m.plays))
  const hasWeekday = $derived(activity.weekday.some((n) => n > 0))

  const tod = $derived.by(() => {
    const t = activity.time_of_day
    if (!t) return null
    const values = PARTS.map((p) => t[p.key])
    const total = values.reduce((s, n) => s + n, 0)
    const dominant = PARTS.find((p) => p.key === t.dominant) ?? PARTS[0]
    return { values, total, dominant, share: Math.round((t[dominant.key] / total) * 100) }
  })

  const facts = $derived.by(() => {
    const out = [{ label: 'Longest streak', value: plural(activity.longest_streak, 'day', 'days') }]
    if (!isPastYear && activity.current_streak != null)
      out.push({ label: 'Current streak', value: plural(activity.current_streak, 'day', 'days') })
    if (activity.tracking_days != null)
      out.push({ label: 'Tracking for', value: plural(activity.tracking_days, 'day', 'days') })
    if (activity.active_days != null)
      out.push({ label: 'Active days', value: plural(activity.active_days, 'day', 'days') })
    return out
  })

  const first = $derived.by(() => {
    const f = activity.first_play
    if (!f) return null
    const date = formatDay(dayOf(f.watched_at))
    return {
      title: f.title,
      sub: f.episode ? `S${f.episode.season}E${f.episode.number} · ${date}` : date,
    }
  })
</script>

<StatSection label="Watch activity" plain>
  <div class="card">
    <p class="card-caption">
      {isYear ? `Plays per month in ${period}` : 'Plays over the last 12 months'}
    </p>
    <StatsBarChart
      labels={monthLabels}
      values={monthValues}
      thinLabels
      tooltipLines={(i) => [
        formatMonth(activity.months[i].month),
        plural(activity.months[i].plays, 'play', 'plays'),
      ]}
      ariaLabel="Plays per month"
    />
  </div>

  <div class="card">
    <PlaysHeatmap
      heatmap={activity.heatmap}
      caption={isYear ? `Plays per day in ${period}` : 'Plays over the last year'}
    />
  </div>

  {#if hasWeekday}
    <div class="card">
      <p class="card-caption">Plays by weekday</p>
      <StatsBarChart
        labels={WEEKDAYS_SHORT}
        values={activity.weekday}
        height={110}
        tooltipLines={(i) => [WEEKDAYS_LONG[i], plural(activity.weekday[i], 'play', 'plays')]}
        ariaLabel="Plays by weekday"
      />
    </div>
  {/if}

  {#if tod}
    <div class="card">
      <p class="card-caption">Plays by time of day</p>
      <StatsBarChart
        labels={PARTS.map((p) => p.label)}
        values={tod.values}
        height={110}
        tooltipLines={(i) => [
          `${PARTS[i].label} (${PARTS[i].range})`,
          plural(tod.values[i], 'play', 'plays'),
          `${Math.round((tod.values[i] / tod.total) * 100)}%`,
        ]}
        ariaLabel="Plays by time of day"
      />
      <dl class="facts">
        <div class="fact">
          <dt class="fact-label">{tod.dominant.persona}</dt>
          <dd class="fact-value">{tod.share}% {tod.dominant.span}</dd>
        </div>
      </dl>
    </div>
  {/if}

  <div class="card">
    <dl class="facts">
      {#each facts as f (f.label)}
        <div class="fact">
          <dt class="fact-label">{f.label}</dt>
          <dd class="fact-value">{f.value}</dd>
        </div>
      {/each}
    </dl>
  </div>

  {#if first}
    <div class="card">
      <h3 class="sub-label first">{isYear ? `First play of ${period}` : 'Where it all started'}</h3>
      <ul class="row-list">
        <TitleRow title={first.title} sub={first.sub} />
      </ul>
    </div>
  {/if}
</StatSection>

<style>
  .facts {
    margin-top: 0.9rem;
  }

  .card > .facts {
    margin-top: 0;
  }

  dd {
    margin: 0;
  }

  .sub-label.first {
    margin-top: 0;
  }
</style>
