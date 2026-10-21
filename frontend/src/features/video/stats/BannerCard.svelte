<script>
  // A backdrop banner naming one title — the Lists hub's card shape (art
  // behind a bottom scrim) with a badge, a heading and a sub-line. The whole
  // art-and-text is the link; an optional action (the unrated hero's Rate)
  // renders as a SIBLING of the link, over the scrim's right end, so a button
  // never nests inside an anchor.

  import { route } from '@mateothegreat/svelte5-router'
  import { BANNER_SIZES, assetUrl, backdropSrcset, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { titleHref, titleName } from './labels.js'

  /**
   * @type {{
   *   title: import('../common/api.js').StatsTitle,
   *   badge: string,
   *   subtitle?: string | null,
   *   heading?: string | null,
   *   ratio?: '2 / 1' | '16 / 9',
   *   sizes?: string,
   *   action?: import('svelte').Snippet,
   * }}
   */
  // `sizes` is the banner's rendered width for srcset — the page-wide default,
  // or what a caller that lays two side by side knows about its grid.
  let {
    title,
    badge,
    subtitle = null,
    heading = null,
    ratio = '2 / 1',
    sizes = BANNER_SIZES,
    action,
  } = $props()

  const name = $derived(heading ?? titleName(title))
</script>

<article class="banner" style="--ratio: {ratio}; --poster-hue: {hueFor(titleName(title))}">
  <a class="banner-link" href={titleHref(title)} use:route>
    <div class="art" aria-hidden="true">
      {#if title.backdrop}
        <img
          src={assetUrl(title.backdrop)}
          srcset={backdropSrcset(title.backdrop, title.backdrop_full)}
          {sizes}
          alt=""
          loading="lazy"
        />
      {:else if title.poster}
        <img
          class="as-poster"
          src={assetUrl(title.poster)}
          srcset={posterSrcset(title.poster, title.poster_full)}
          {sizes}
          alt=""
          loading="lazy"
        />
      {:else}
        <span class="initials">{initialsFor(titleName(title))}</span>
      {/if}
    </div>
    <div class="caption">
      <span class="badge">{badge}</span>
      <h3>{name}</h3>
      {#if subtitle}
        <span class="sub">{subtitle}</span>
      {/if}
    </div>
  </a>
  {#if action}
    <div class="banner-action">
      {@render action()}
    </div>
  {/if}
</article>

<style>
  .banner {
    position: relative;
    aspect-ratio: var(--ratio);
    overflow: hidden;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    background: var(--surface);
    box-shadow: var(--shadow-card);
    transition:
      transform var(--ease-lift),
      box-shadow var(--ease-lift),
      border-color var(--ease-lift);
  }

  .banner-link {
    position: absolute;
    inset: 0;
    display: block;
    color: var(--text);
    text-decoration: none;
  }

  .banner-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  @media (hover: hover) {
    .banner:hover {
      transform: translateY(-3px);
      box-shadow: var(--shadow-lift);
      border-color: var(--accent);
    }

    .banner:hover img {
      transform: scale(1.05);
    }
  }

  .art {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background:
      radial-gradient(circle at 30% 20%, hsl(var(--poster-hue) 45% 30%), transparent 60%),
      linear-gradient(135deg, hsl(var(--poster-hue) 40% 22%), hsl(var(--poster-hue) 45% 10%));
  }

  .art img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    transition: transform 0.4s ease;
  }

  /* A poster standing in for a missing backdrop: blurred and dimmed so the
     caption stays readable, the way the detail heroes treat it. */
  .art img.as-poster {
    filter: blur(14px) brightness(0.6);
    transform: scale(1.1);
  }

  .initials {
    font-size: 2.5rem;
    font-weight: 700;
    color: rgb(255 255 255 / 0.35);
    margin-bottom: 12%;
  }

  .caption {
    position: absolute;
    inset: auto 0 0 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    /* Room on the right for the action, which sits over the same scrim. */
    padding: 2.5rem 7rem 0.9rem 1rem;
    background: linear-gradient(to top, rgb(0 0 0 / 0.85), rgb(0 0 0 / 0.55) 55%, transparent);
  }

  .badge {
    color: var(--accent-soft);
    font-size: 0.65rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  h3 {
    margin: 0;
    font-size: 1.2rem;
    line-height: 1.2;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    color: rgb(255 255 255 / 0.75);
    font-size: 0.8rem;
  }

  .banner-action {
    position: absolute;
    right: 1rem;
    bottom: 0.9rem;
  }

  @media (max-width: 599px) {
    .banner {
      aspect-ratio: 16 / 9;
    }
  }
</style>
