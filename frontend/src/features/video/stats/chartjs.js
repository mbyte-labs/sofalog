// The ONE importer of chart.js in the SPA. Registering only what a bar chart
// needs keeps the chunk small, and StatsBarChart.svelte imports this module
// lazily, so no route but the Stats page ever loads it.
import { Chart, BarController, BarElement, CategoryScale, LinearScale, Tooltip } from 'chart.js'

Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip)

export { Chart }
