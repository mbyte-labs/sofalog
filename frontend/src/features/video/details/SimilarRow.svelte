<script>
  // The "Similar" rail both detail pages render: TMDB's recommendations for
  // the item, read off its own snapshot (D-024) — so a tracked title keeps its
  // rail with TMDB down, exactly like its cast. One component, because the two
  // pages would otherwise render identical markup, like CastRow.
  //
  // Every entry is a title the library has no row for, so a card links by
  // tmdb_id and its poster is the cache proxy's — the search card's contract,
  // not the asset tree's. The list is the same kind as the page it hangs off
  // (a show's recommendations are shows), which is why `kind` picks the route
  // rather than each entry carrying a media type.
  //
  // No "+N more" expander: TMDB sends one page of at most 20, which is one
  // rail — the cast rail's page size — so there is never anything to hide.

  import { route } from '@mateothegreat/svelte5-router'
  import { RAIL_POSTER_SIZES, assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'

  /**
   * @type {{
   *   items: import('../common/api.js').SimilarTitle[],
   *   kind: 'movie' | 'show',
   *   emptyNote: string,
   * }}
   */
  let { items, kind, emptyNote } = $props()

  const label = $derived(kind === 'movie' ? 'Similar movies' : 'Similar shows')
</script>

{#if items.length > 0}
  <div class="scroll-row" aria-label={label}>
    {#each items as item (item.tmdb_id)}
      <!-- The whole card is the link, as on the search results and the season
           grid. Hue seeded from the entry's own title, so a poster-less card
           gets the same tint here as it would on the search page. -->
      <a
        class="similar-card"
        href="/video/{kind}-details?tmdb_id={item.tmdb_id}"
        use:route
        style="--poster-hue: {hueFor(item.title)}"
      >
        <div class="similar-poster">
          {#if item.poster}
            <img
              src={assetUrl(item.poster)}
              srcset={posterSrcset(item.poster, item.poster_full)}
              sizes={RAIL_POSTER_SIZES}
              alt=""
              loading="lazy"
            />
          {:else}
            <span class="similar-initials">{initialsFor(item.title)}</span>
          {/if}
          {#if item.rating != null}
            <!-- TMDB's community score, never this user's — a recommended
                 title has no user state to show. -->
            <span class="similar-rating">
              <span class="heart">♥</span>
              {item.rating.toFixed(1)}
            </span>
          {/if}
        </div>
        <p class="similar-name" title={item.title}>{item.title}</p>
        {#if item.year}
          <p class="similar-meta">{item.year}</p>
        {/if}
      </a>
    {/each}
  </div>
{:else}
  <p class="row-note pending">{emptyNote}</p>
{/if}
