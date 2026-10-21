<script>
  // §3.6: the user's average against TMDB's, and the titles they parted
  // ways on.
  import StatSection from './StatSection.svelte'
  import TitleRow from './TitleRow.svelte'
  import { signedDelta } from '../common/format.js'
  import { crowdVerdict } from './labels.js'

  /** @type {{ crowd: import('../common/api.js').StatsCrowd }} */
  let { crowd } = $props()

  const rounded = $derived(Math.round(crowd.delta * 10) / 10)
</script>

<StatSection label="You vs the crowd">
  <div class="verdict">
    <span class="big delta" class:up={rounded > 0} class:down={rounded < 0}>
      {signedDelta(crowd.delta)}
    </span>
    <span class="verdict-text">{crowdVerdict(crowd.verdict)}</span>
  </div>
  <dl class="facts">
    <div class="fact">
      <dt class="fact-label">Your average</dt>
      <dd class="fact-value">{crowd.user_average.toFixed(1)}</dd>
    </div>
    <div class="fact">
      <dt class="fact-label">TMDB average</dt>
      <dd class="fact-value">{crowd.crowd_average.toFixed(1)}</dd>
    </div>
    <div class="fact">
      <dt class="fact-label">Compared</dt>
      <dd class="fact-value">{crowd.compared} of {crowd.total_rated} rated titles</dd>
    </div>
  </dl>

  <div class="lists">
    {#if crowd.loved_more.length > 0}
      <div>
        <h3 class="sub-label">You loved, the crowd didn't</h3>
        <ul class="row-list">
          {#each crowd.loved_more as e (e.title.kind + e.title.id)}
            <TitleRow
              title={e.title}
              sub="{e.title.year
                ? `${e.title.year} · `
                : ''}You {e.user_rating} · TMDB {e.tmdb_average.toFixed(1)}"
              figure={signedDelta(e.gap)}
              figureTone="up"
            />
          {/each}
        </ul>
      </div>
    {/if}
    {#if crowd.loved_less.length > 0}
      <div>
        <h3 class="sub-label">The crowd loved, you didn't</h3>
        <ul class="row-list">
          {#each crowd.loved_less as e (e.title.kind + e.title.id)}
            <TitleRow
              title={e.title}
              sub="{e.title.year
                ? `${e.title.year} · `
                : ''}You {e.user_rating} · TMDB {e.tmdb_average.toFixed(1)}"
              figure={signedDelta(e.gap)}
              figureTone="down"
            />
          {/each}
        </ul>
      </div>
    {/if}
  </div>
</StatSection>

<style>
  .verdict {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .big {
    font-size: 1.8rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
  }

  .verdict-text {
    font-weight: 600;
  }

  .facts {
    margin-top: 0.9rem;
  }

  dd {
    margin: 0;
  }

  .lists {
    display: grid;
    gap: 0 1.5rem;
  }

  @media (min-width: 1100px) {
    .lists {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
