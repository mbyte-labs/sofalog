<script>
  // A compact row for one title: an optional rank, a poster thumb, the name
  // and a sub-line, a right-aligned figure, an optional meter under the text,
  // and an optional action rendered beside — never inside — the link.

  import { route } from '@mateothegreat/svelte5-router'
  import { assetUrl, posterSrcset } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { titleHref, titleName } from './labels.js'

  /**
   * @type {{
   *   title: import('../common/api.js').StatsTitle,
   *   rank?: number | null,
   *   sub?: string | null,
   *   figure?: string | null,
   *   figureTone?: 'accent' | 'up' | 'down' | null,
   *   meter?: number | null,
   *   action?: import('svelte').Snippet,
   * }}
   */
  let {
    title,
    rank = null,
    sub = null,
    figure = null,
    figureTone = null,
    meter = null,
    action,
  } = $props()

  const name = $derived(titleName(title))
</script>

<li class="trow" style="--poster-hue: {hueFor(name)}">
  {#if rank != null}
    <span class="trow-rank" aria-label="Rank {rank}">{rank}</span>
  {/if}
  <a class="trow-link" href={titleHref(title)} use:route>
    <span class="trow-poster">
      {#if title.poster}
        <img
          src={assetUrl(title.poster)}
          srcset={posterSrcset(title.poster, title.poster_full)}
          sizes="48px"
          alt=""
          loading="lazy"
        />
      {:else}
        <span class="trow-initials">{initialsFor(name)}</span>
      {/if}
    </span>
    <span class="trow-text">
      <span class="trow-name">{name}</span>
      {#if sub}
        <span class="trow-sub">{sub}</span>
      {/if}
      {#if meter != null}
        <span class="meter" aria-hidden="true">
          <span class="meter-fill cta" style="width: {Math.round(meter * 100)}%"></span>
        </span>
      {/if}
    </span>
  </a>
  {#if figure}
    <span class="trow-figure {figureTone ?? ''}">{figure}</span>
  {/if}
  {#if action}
    <span class="trow-action">{@render action()}</span>
  {/if}
</li>

<style>
  .trow {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.35rem 0.4rem;
    border-radius: var(--radius-md);
  }

  @media (hover: hover) {
    .trow:hover {
      background: var(--bg);
    }
  }

  .trow-rank {
    flex: none;
    min-width: 1.4rem;
    color: var(--text-dim);
    font-size: 0.85rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    text-align: right;
  }

  .trow-link {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text);
    text-decoration: none;
  }

  .trow-link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-sm);
  }

  .trow-poster {
    flex: none;
    width: 3rem;
    aspect-ratio: 2 / 3;
    display: grid;
    place-items: center;
    overflow: hidden;
    border-radius: var(--radius-sm);
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 40% 32%),
      hsl(var(--poster-hue) 45% 14%)
    );
  }

  .trow-poster img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .trow-initials {
    font-size: 0.85rem;
    font-weight: 700;
    color: rgb(255 255 255 / 0.7);
  }

  .trow-text {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
    flex: 1 1 auto;
  }

  .trow-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .trow-sub {
    color: var(--text-dim);
    font-size: 0.8rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .trow-text .meter {
    display: block;
    max-width: 16rem;
    margin-top: 0.25rem;
  }

  .trow-text .meter-fill {
    display: block;
  }

  .trow-figure {
    flex: none;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }

  .trow-figure.accent {
    color: var(--accent);
  }

  .trow-figure.up {
    color: var(--ok);
  }

  .trow-figure.down {
    color: var(--err);
  }

  .trow-action {
    flex: none;
  }

  @media (hover: none) {
    .trow {
      min-height: 3.25rem;
    }
  }
</style>
