<script>
  // The hero both detail pages open with: the item's poster, blurred and
  // oversized, as a full-bleed backdrop behind the poster itself.
  //
  // The text is snippets rather than props because the anatomy is shared but
  // the copy is not — a movie leads with its director and runtime, a show with
  // its creator and season count. The rules that dress that snippet markup
  // live in detail-page.css: a component can only scope styles to markup it
  // declares itself.

  import { assetUrl } from '../../../../lib/images.js'
  import { hueFor, initialsFor } from '../posterPlaceholder.js'

  /**
   * `wide` switches the art slot from a 2:3 poster to a 16:9 still — the
   * episode page's shape. Everything else about the hero is identical, so it
   * is one prop rather than a second component.
   *
   * @type {{
   *   title: string,
   *   poster: string | null,
   *   posterFull: string | null,
   *   wide?: boolean,
   *   info: import('svelte').Snippet,
   *   desc: import('svelte').Snippet,
   * }}
   */
  let { title, poster, posterFull, wide = false, info, desc } = $props()

  // The detail size when it exists, so the backdrop blur has pixels to work
  // with and the poster is not upscaled.
  const art = $derived(posterFull ?? poster)
</script>

<!-- Full-bleed hero: escapes <main>'s padding with negative margins and
     re-centers its own content, so the backdrop runs edge to edge and the
     scrim fades it seamlessly into the page background. -->
<header class="hero" style="--poster-hue: {hueFor(title)}">
  {#if art}
    <img class="hero-backdrop" src={assetUrl(art)} alt="" aria-hidden="true" />
  {/if}
  <div class="hero-scrim"></div>

  <div class="hero-inner">
    <div class="hero-poster" class:wide>
      {#if art}
        <img src={assetUrl(art)} alt="" />
      {:else}
        <div class="poster-placeholder">
          <span>{initialsFor(title)}</span>
        </div>
      {/if}
    </div>

    <div class="hero-info">
      {@render info()}
    </div>

    <div class="hero-desc">
      {@render desc()}
    </div>
  </div>
</header>

<style>
  /* Escape <main>'s padding (values mirror App.svelte) so the backdrop runs
       from the sidebar edge to the right edge of the window with no margins. */
  .hero {
    position: relative;
    overflow: hidden;
    margin-top: calc(-1 * var(--space-page));
    margin-left: calc(-1 * max(var(--space-page), env(safe-area-inset-left)));
    margin-right: calc(-1 * max(var(--space-page), env(safe-area-inset-right)));
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 35% 18%),
      hsl(var(--poster-hue) 40% 8%)
    );
  }

  /* Backdrops are stored but not yet served (the poster proxy whitelists only
       the two poster sizes), so the poster itself — blurred, darkened and
       oversized — plays the part. Oversizing hides the blur's faded edges. */
  .hero-backdrop {
    position: absolute;
    inset: -10%;
    width: 120%;
    height: 120%;
    object-fit: cover;
    filter: blur(40px) brightness(0.55) saturate(1.15);
  }

  /* Fades to fully opaque --bg at the bottom, so the hero dissolves into the
       page with no visible seam. */
  .hero-scrim {
    position: absolute;
    inset: 0;
    background: linear-gradient(
      180deg,
      rgb(31 30 29 / 0.1),
      rgb(31 30 29 / 0.55) 55%,
      var(--bg) 100%
    );
  }

  /* Content re-centers itself inside the full-bleed band. Grid areas regroup
       per breakpoint: mobile/medium stacks poster+info / desc (the side rail is
       desktop-only), desktop puts the side rail top-right like the reference
       design. */
  .hero-inner {
    position: relative;
    display: grid;
    grid-template:
      'poster info' auto
      'desc desc' auto
      / auto minmax(0, 1fr);
    gap: 1.25rem;
    padding: 2rem var(--space-page) 1.5rem;
  }

  .hero-poster {
    grid-area: poster;
    width: clamp(110px, 26vw, 240px);
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-md);
    overflow: hidden;
    border: 1px solid rgb(255 255 255 / 0.14);
    box-shadow: var(--shadow-lift);
    background: var(--surface);
  }

  /* An episode's still is 16:9 and wants the width a poster's height would
     have taken. */
  .hero-poster.wide {
    width: clamp(220px, 46vw, 460px);
    aspect-ratio: 16 / 9;
  }

  .hero-poster img,
  .poster-placeholder {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .poster-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 40% 32%),
      hsl(var(--poster-hue) 45% 14%)
    );
  }

  .poster-placeholder span {
    font-size: 2.4rem;
    font-weight: 700;
    color: rgb(255 255 255 / 0.55);
    letter-spacing: 0.05em;
  }

  .hero-info {
    grid-area: info;
    min-width: 0;
    align-self: end;
  }

  .hero-desc {
    grid-area: desc;
    max-width: 70ch;
  }

  @media (min-width: 1100px) {
    /* Content re-centers itself inside the full-bleed band. Grid areas regroup
         per breakpoint: mobile/medium stacks poster+info / desc (the side rail is
         desktop-only), desktop puts the side rail top-right like the reference
         design. */
    .hero-inner {
      grid-template:
        'poster info' auto
        'poster desc' 1fr
        / auto minmax(0, 1fr);
      column-gap: 2.5rem;
      row-gap: 1rem;
      padding-top: 2.5rem;
      padding-bottom: 2rem;
    }

    .hero-poster {
      width: clamp(240px, 22vw, 340px);
    }

    .hero-poster.wide {
      width: clamp(360px, 38vw, 620px);
    }

    .hero-info {
      align-self: end;
    }

    .hero-desc {
      align-self: start;
    }
  }

  @media (max-width: 599px) {
    /* Content re-centers itself inside the full-bleed band. Grid areas regroup
         per breakpoint: mobile/medium stacks poster+info / desc (the side rail is
         desktop-only), desktop puts the side rail top-right like the reference
         design. */
    .hero-inner {
      padding-top: 1.25rem;
      padding-bottom: 1rem;
      gap: 1rem;
    }
  }
</style>
