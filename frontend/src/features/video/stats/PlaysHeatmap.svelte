<script>
  // The daily heatmap: one column per week with Monday at the top, a shade
  // per day from the server's quartile levels, month labels over each 1st,
  // and a tap that names a day. Plain CSS grid — a calendar is a grid, not a
  // chart. Every day here is a YYYY-MM-DD the server already bucketed in the
  // user's zone, so the arithmetic below runs on UTC midnights and no zone,
  // the device's included, can move a cell.

  import { formatMonth, formatWeekdayDay, plural } from '../common/format.js'

  /** @type {{ heatmap: import('../common/api.js').StatsHeatmap, caption: string }} */
  let { heatmap, caption } = $props()

  /** @param {string} day YYYY-MM-DD */
  function utcMs(day) {
    const [y, m, d] = day.split('-').map(Number)
    return Date.UTC(y, m - 1, d)
  }

  const DAY_MS = 86_400_000

  const grid = $derived.by(() => {
    const startMs = utcMs(heatmap.start)
    // Monday = 0 … Sunday = 6, from the UTC weekday of the anchored day.
    const lead = (new Date(startMs).getUTCDay() + 6) % 7
    const cells = heatmap.plays.map((plays, i) => {
      const day = new Date(startMs + i * DAY_MS).toISOString().slice(0, 10)
      return {
        i,
        day,
        plays,
        level: heatmap.levels[i] ?? 0,
        col: Math.floor((lead + i) / 7) + 1,
        row: ((lead + i) % 7) + 1,
      }
    })
    const weeks = cells.at(-1)?.col ?? 0
    // A month label over each 1st, dropped when it would sit closer than
    // three columns to the previous one.
    /** @type {{ col: number, label: string }[]} */
    const months = []
    let last = -Infinity
    for (const c of cells) {
      if (c.day.endsWith('-01') && c.col - last >= 3) {
        months.push({ col: c.col, label: formatMonth(c.day.slice(0, 7), true) })
        last = c.col
      }
    }
    let totalPlays = 0
    let activeDays = 0
    for (const c of cells) {
      totalPlays += c.plays
      if (c.plays > 0) activeDays++
    }
    return { cells, weeks, months, totalPlays, activeDays }
  })

  /** @type {number | null} the selected cell's index */
  let selected = $state(null)
  /** @type {HTMLDivElement | undefined} */
  let scroller = $state()

  // New data: forget the selection and open on the latest weeks. Effects run
  // after the DOM updated, so the scroll width is the new grid's.
  $effect(() => {
    void heatmap
    selected = null
    if (scroller) scroller.scrollLeft = scroller.scrollWidth
  })

  /** @param {MouseEvent} event */
  function pick(event) {
    const target = /** @type {HTMLElement | null} */ (event.target)
    const cell = target?.closest('[data-i]')
    if (!cell) return
    selected = Number(/** @type {HTMLElement} */ (cell).dataset.i)
  }

  const selectedCell = $derived(selected == null ? null : grid.cells[selected])
</script>

<p class="card-caption">{caption}</p>

<div class="hm-scroll" bind:this={scroller}>
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
  <div
    class="hm"
    style="--weeks: {grid.weeks}"
    role="img"
    aria-label="Plays per day: {plural(grid.totalPlays, 'play', 'plays')} on {plural(
      grid.activeDays,
      'day',
      'days',
    )}"
    onclick={pick}
  >
    {#each grid.months as m (m.col)}
      <span class="hm-month" style="grid-column: {m.col + 1}">{m.label}</span>
    {/each}
    <span class="hm-gutter" style="grid-row: 2">Mon</span>
    <span class="hm-gutter" style="grid-row: 4">Wed</span>
    <span class="hm-gutter" style="grid-row: 6">Fri</span>
    {#each grid.cells as c (c.day)}
      <div
        class="hm-cell s{c.level}"
        class:selected={selected === c.i}
        style="grid-column: {c.col + 1}; grid-row: {c.row + 1}"
        data-i={c.i}
        title="{formatWeekdayDay(c.day)} · {plural(c.plays, 'play', 'plays')}"
      ></div>
    {/each}
  </div>
</div>

<p class="hm-selected" aria-live="polite">
  {#if selectedCell}
    {formatWeekdayDay(selectedCell.day)} · {plural(selectedCell.plays, 'play', 'plays')}
  {:else}
    Tap a day for its plays
  {/if}
</p>

<div class="hm-footer">
  <span>{plural(grid.totalPlays, 'play', 'plays')} on {plural(grid.activeDays, 'day', 'days')}</span
  >
  <span class="hm-legend" aria-hidden="true">
    Less
    <span class="hm-cell s0"></span>
    <span class="hm-cell s1"></span>
    <span class="hm-cell s2"></span>
    <span class="hm-cell s3"></span>
    <span class="hm-cell s4"></span>
    More
  </span>
</div>

<style>
  .hm-scroll {
    overflow-x: auto;
    overscroll-behavior-x: contain;
    padding-bottom: 0.35rem;
  }

  .hm {
    --cell: 12px;
    display: grid;
    grid-template-columns: 2rem repeat(var(--weeks), var(--cell));
    grid-template-rows: 1rem repeat(7, var(--cell));
    gap: 3px;
    width: max-content;
  }

  .hm-month {
    grid-row: 1;
    color: var(--text-dim);
    font-size: 0.65rem;
    line-height: 1rem;
    white-space: nowrap;
  }

  /* The weekday gutter stays put while the weeks scroll under it. */
  .hm-gutter {
    position: sticky;
    left: 0;
    grid-column: 1;
    padding-right: 0.3rem;
    background: var(--surface);
    color: var(--text-dim);
    font-size: 0.65rem;
    line-height: var(--cell);
  }

  .hm-cell {
    display: inline-block;
    width: var(--cell);
    height: var(--cell);
    border-radius: 2px;
    cursor: pointer;
  }

  /* Shades: the accent's own tints (app.css's rule — one rgb triplet at
     rising alpha), the busiest days the accent itself. */
  .hm-cell.s0 {
    background: var(--bg);
    border: 1px solid var(--border);
  }

  .hm-cell.s1 {
    background: var(--accent-tint);
  }

  .hm-cell.s2 {
    background: var(--accent-tint-strong);
  }

  .hm-cell.s3 {
    background: rgb(14 165 233 / 0.6);
  }

  .hm-cell.s4 {
    background: var(--accent);
  }

  .hm-cell.selected {
    outline: 2px solid var(--text);
    outline-offset: 1px;
  }

  .hm-selected {
    margin: 0.5rem 0 0;
    color: var(--text-dim);
    font-size: 0.8rem;
    font-variant-numeric: tabular-nums;
  }

  .hm-footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.5rem 1rem;
    margin-top: 0.5rem;
    color: var(--text-dim);
    font-size: 0.75rem;
  }

  .hm-legend {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .hm-legend .hm-cell {
    --cell: 10px;
    cursor: default;
  }
</style>
