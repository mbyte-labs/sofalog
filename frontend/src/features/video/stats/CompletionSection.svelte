<script>
  // §3.11: how the followed shows stand, and the ones closest to done.
  import StatSection from './StatSection.svelte'
  import TitleRow from './TitleRow.svelte'
  import { plural } from '../common/format.js'
  import { percent } from './labels.js'

  /** @type {{ completion: import('../common/api.js').StatsCompletion }} */
  let { completion } = $props()
</script>

<StatSection label="Show completion">
  <div class="avg">
    <div class="avg-head">
      <span class="fact-label">Average completion</span>
      <span class="fact-value">{percent(completion.average_completion)}</span>
    </div>
    <div class="meter" aria-hidden="true">
      <div
        class="meter-fill cta"
        style="width: {Math.round(completion.average_completion * 100)}%"
      ></div>
    </div>
  </div>
  <dl class="facts">
    <div class="fact">
      <dt class="fact-label">Shows tracked</dt>
      <dd class="fact-value">{completion.tracked}</dd>
    </div>
    <div class="fact">
      <dt class="fact-label">Completed</dt>
      <dd class="fact-value">{completion.completed}</dd>
    </div>
    <div class="fact">
      <dt class="fact-label">In progress</dt>
      <dd class="fact-value">{completion.in_progress}</dd>
    </div>
  </dl>

  {#if completion.almost_there.length > 0}
    <h3 class="sub-label">Almost there</h3>
    <ul class="row-list">
      {#each completion.almost_there as s (s.show.id)}
        <TitleRow
          title={s.show}
          sub="{plural(s.left, 'episode left', 'episodes left')} · {s.watched} of {s.aired} watched"
          meter={s.aired > 0 ? s.watched / s.aired : 0}
        />
      {/each}
    </ul>
  {/if}
</StatSection>

<style>
  .avg-head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 0.4rem;
  }

  .facts {
    margin-top: 1rem;
  }

  dd {
    margin: 0;
  }
</style>
