<script>
  // §3.4: a horizontal rail of the highest-rated movies and shows.
  import StatSection from './StatSection.svelte'
  import PosterTile from './PosterTile.svelte'

  /** @type {{ items: import('../common/api.js').StatsTopRated[] }} */
  let { items } = $props()
</script>

<StatSection label="Your highest rated" plain>
  <div class="scroll-row" aria-label="Your highest rated">
    {#each items as item (item.title.kind + item.title.id)}
      <PosterTile title={item.title} rating={item.rating} />
    {/each}
  </div>
</StatSection>
