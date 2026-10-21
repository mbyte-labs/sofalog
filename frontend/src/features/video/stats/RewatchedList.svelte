<script>
  // §3.10: the titles played more than once, ranked.
  import StatSection from './StatSection.svelte'
  import TitleRow from './TitleRow.svelte'

  /** @type {{ items: import('../common/api.js').StatsRewatched[] }} */
  let { items } = $props()
</script>

<StatSection label="Most rewatched">
  <ul class="row-list">
    {#each items as item, i (item.title.kind + item.title.id)}
      <TitleRow
        title={item.title}
        rank={i + 1}
        sub={item.title.year ? String(item.title.year) : null}
        figure="{item.plays}×"
        figureTone="accent"
      />
    {/each}
  </ul>
</StatSection>
