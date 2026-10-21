<script>
  // One section of the Stats page: an uppercase label, an optional caption,
  // and its body — boxed in a card unless the body brings its own surfaces
  // (banners, a rail, a stack of cards).
  /** @type {{ label: string, caption?: string | null, plain?: boolean, children: import('svelte').Snippet }} */
  let { label, caption = null, plain = false, children } = $props()
</script>

<section class="section">
  <h2 class="section-label">{label}</h2>
  {#if caption}
    <p class="section-caption">{caption}</p>
  {/if}
  {#if plain}
    {@render children()}
  {:else}
    <div class="card">
      {@render children()}
    </div>
  {/if}
</section>
