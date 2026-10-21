<script module>
  // One import per page life: the chunk is fetched when the first chart
  // mounts and shared by every chart after it.
  /** @type {Promise<typeof import('./chartjs.js')> | undefined} */
  let loader
  const loadChart = () => (loader ??= import('./chartjs.js'))
</script>

<script>
  // The one Chart.js wrapper the Stats sections share: a single bar series in
  // the brand colour with rounded tops, a hairline horizontal grid, whole
  // number ticks, no legend, and a tooltip whose lines the caller writes. The
  // library is themed purely through its options — every colour and the font
  // are read off the host element's computed style at mount, so the chart
  // looks like the app without touching the library.

  /**
   * @type {{
   *   labels: string[],
   *   values: number[],
   *   height?: number,
   *   fixedMax?: number | null,
   *   colors?: string[] | null,
   *   tooltipLines: (i: number) => string[],
   *   thinLabels?: boolean,
   *   ariaLabel: string,
   * }}
   */
  let {
    labels,
    values,
    // The host's height in px; the canvas fills it (maintainAspectRatio off).
    height = 180,
    // A fixed y-axis top (the 0–10 rating scale); null = tallest × 1.15.
    fixedMax = null,
    // Per-bar colours (the rating distribution); null = the accent for all.
    colors = null,
    // The tooltip's lines for bar i: the first is the title, the rest the body.
    tooltipLines,
    // Label every other bar, so a 12-month axis never collides.
    thinLabels = false,
    ariaLabel,
  } = $props()

  /** @type {HTMLDivElement | undefined} */
  let host = $state()
  /** @type {HTMLCanvasElement | undefined} */
  let canvas = $state()
  // Plain, not $state: Chart.js owns this object, and a proxy around it would
  // slow every frame for nothing.
  /** @type {import('chart.js').Chart<'bar'> | null} */
  let chart = null

  function theme() {
    const cs = getComputedStyle(host ?? document.documentElement)
    /** @param {string} name */
    const v = (name) => cs.getPropertyValue(name).trim()
    return {
      accent: v('--accent'),
      border: v('--border'),
      dim: v('--text-dim'),
      text: v('--text'),
      surface: v('--surface'),
      font: cs.fontFamily,
    }
  }

  /**
   * A canvas cannot paint "var(--accent)", which is what ratingColor() hands
   * back for 8–9; resolve a var() through the same computed style.
   * @param {string} color @param {ReturnType<typeof theme>} t
   */
  function resolve(color, t) {
    const m = /^var\((--[a-z-]+)\)$/.exec(color)
    if (!m) return color
    const cs = getComputedStyle(host ?? document.documentElement)
    return cs.getPropertyValue(m[1]).trim() || t.accent
  }

  /** @param {number[]} vals */
  function yMax(vals) {
    if (fixedMax != null) return fixedMax
    const top = Math.max(0, ...vals)
    return top === 0 ? 1 : Math.ceil(top * 1.15)
  }

  /** @returns {import('chart.js').ChartConfiguration<'bar'>} */
  function config() {
    const t = theme()
    const lines = tooltipLines
    const labelAt = labels
    const thin = thinLabels
    const max = yMax(values)
    // Plain copies: Chart.js defines properties on its data arrays, which a
    // $state proxy (a section's array handed straight through) refuses.
    return {
      type: 'bar',
      data: {
        labels: [...labels],
        datasets: [
          {
            data: [...values],
            backgroundColor: colors ? colors.map((c) => resolve(c, t)) : t.accent,
            // Rounded tops only: the base edge keeps its corners.
            borderRadius: 4,
            borderSkipped: 'bottom',
            maxBarThickness: 32,
            categoryPercentage: 0.8,
            barPercentage: 0.9,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: matchMedia('(prefers-reduced-motion: reduce)').matches
          ? false
          : { duration: 250 },
        // A tap anywhere in a bar's column shows its tooltip — on touch the
        // bar itself is a small target.
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: false },
          tooltip: {
            displayColors: false,
            backgroundColor: t.surface,
            borderColor: t.border,
            borderWidth: 1,
            titleColor: t.text,
            bodyColor: t.dim,
            cornerRadius: 6,
            padding: 8,
            titleFont: { family: t.font, size: 12, weight: 600 },
            bodyFont: { family: t.font, size: 12 },
            callbacks: {
              title: (items) => lines(items[0].dataIndex)[0],
              label: (item) => lines(item.dataIndex).slice(1),
            },
          },
        },
        scales: {
          x: {
            grid: { display: false },
            border: { display: false },
            ticks: {
              color: t.dim,
              font: { family: t.font, size: 11 },
              maxRotation: 0,
              autoSkip: false,
              callback: (_, i) => (thin && i % 2 === 1 ? '' : labelAt[i]),
            },
          },
          y: {
            beginAtZero: true,
            max,
            grid: { color: t.border, lineWidth: 1 },
            border: { display: false },
            ticks: {
              color: t.dim,
              precision: 0,
              maxTicksLimit: 5,
              font: { family: t.font, size: 11 },
              ...(fixedMax != null ? { stepSize: fixedMax / 5 } : {}),
            },
          },
        },
      },
    }
  }

  // Create once the canvas exists. The import resolves asynchronously, so the
  // .then reads the props as they are THEN — nothing inside it is tracked.
  $effect(() => {
    const el = canvas
    if (!el) return
    let alive = true
    loadChart().then(({ Chart }) => {
      if (alive) chart = new Chart(el, config())
    })
    return () => {
      alive = false
      chart?.destroy()
      chart = null
    }
  })

  // Data changes after mount (the silent refetch after a rating): push the
  // new config into the live chart rather than rebuilding the canvas.
  $effect(() => {
    // Tracked reads, so the effect re-runs when any of them changes.
    void labels
    void values
    void colors
    void fixedMax
    if (!chart) return
    const next = config()
    chart.data = next.data
    chart.options = next.options ?? {}
    chart.update()
  })
</script>

<div class="chart-host" bind:this={host} style="height: {height}px">
  <canvas bind:this={canvas} aria-label={ariaLabel}>
    <!-- The canvas's fallback content is what a screen reader gets. -->
    <ul>
      {#each labels as l, i (i)}
        <li>{l}: {values[i]}</li>
      {/each}
    </ul>
  </canvas>
</div>

<style>
  /* Chart.js's responsive mode needs a positioned, explicitly sized host
     dedicated to the canvas. */
  .chart-host {
    position: relative;
    width: 100%;
  }
</style>
