<script>
  // The cast rail both detail pages render — the movie's credited cast and the
  // show's aggregate cast (everyone across every episode, each with an episode
  // count). One component, because the two pages rendered identical markup.
  //
  // The API returns the whole cast: capping is presentation, and only the
  // client knows how much room it has. A show's aggregate cast runs to a
  // hundred-plus people, more than anyone scrolls past on the way to the
  // seasons, so the rail opens with one screenful and puts the rest behind a
  // "+N more" tile — the season grid's expander, poster-shaped like the cards
  // around it. One-way, like the grid's.
  //
  // A card with a person id is a link to the person page (D-027) — the whole
  // card, as on the similar-titles rail. A snapshot written without ids (a
  // hand-made fixture; TMDB always sends them) renders the same card as a
  // plain block, so the rail never offers a link that leads nowhere.

  import { route } from '@mateothegreat/svelte5-router'
  import { assetUrl } from '../../../lib/images.js'
  import { hueFor } from '../common/posterPlaceholder.js'

  /**
   * @type {{
   *   cast: import('../common/api.js').CastMember[],
   *   title: string,
   *   emptyNote: string,
   * }}
   */
  let { cast, title, emptyNote } = $props()

  const CAST_PAGE = 20
  let all = $state(false)

  const visible = $derived(all ? cast : cast.slice(0, CAST_PAGE))
  const hidden = $derived(cast.length - visible.length)

  /** @param {number} n */
  function episodesLabel(n) {
    return `${n} episode${n === 1 ? '' : 's'}`
  }
</script>

{#snippet card(/** @type {import('../common/api.js').CastMember} */ member)}
  <div class="cast-photo">
    {#if member.photo}
      <img src={assetUrl(member.photo)} alt="" loading="lazy" />
    {:else}
      <svg viewBox="0 0 24 24" width="42" height="42" aria-hidden="true">
        <circle cx="12" cy="8.5" r="3.5" fill="currentColor" />
        <path d="M4.5 20.2a7.5 7.5 0 0 1 15 0" fill="currentColor" />
      </svg>
    {/if}
  </div>
  <p class="cast-name" title={member.name}>{member.name}</p>
  {#if member.character}
    <p class="cast-role" title={member.character}>{member.character}</p>
  {/if}
  {#if member.episodes != null}
    <!-- A show figure only; a movie's cast has none. Reads under the
         part like TMDB's own cards do. -->
    <p class="cast-role cast-episodes">{episodesLabel(member.episodes)}</p>
  {/if}
{/snippet}

{#if cast.length > 0}
  <div class="scroll-row" aria-label="Cast">
    {#each visible as member, i (i)}
      {#if member.tmdb_id != null}
        <a
          class="cast-card cast-link"
          href="/video/person-details?tmdb_id={member.tmdb_id}"
          use:route
          style="--poster-hue: {(hueFor(title) + i * 40) % 360}"
        >
          {@render card(member)}
        </a>
      {:else}
        <div class="cast-card" style="--poster-hue: {(hueFor(title) + i * 40) % 360}">
          {@render card(member)}
        </div>
      {/if}
    {/each}
    {#if hidden > 0}
      <button
        class="cast-card cast-more"
        onclick={() => (all = true)}
        aria-label="Show {hidden} more cast {hidden === 1 ? 'member' : 'members'}"
      >
        <span class="cast-photo">
          <span class="more-count">+{hidden}</span>
          <span class="more-label">more</span>
        </span>
      </button>
    {/if}
  </div>
{:else}
  <p class="row-note pending">{emptyNote}</p>
{/if}
