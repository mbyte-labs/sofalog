<script>
  // The media section's tabbed gallery and its lightbox: backdrops, videos and
  // posters for one item. Identical on every detail page, and the fiddliest
  // block on them — tab defaulting, keyboard navigation, focus return and the
  // body scroll lock all have to agree — so it exists once.
  //
  // The section heading stays with the page; this owns everything below it.

  import { assetUrl, backdropSrcset, posterSrcset } from '../../../lib/images.js'
  import { hueFor } from '../common/posterPlaceholder.js'
  import { formatVideoDate } from '../common/format.js'

  /**
   * @type {{
   *   title: string,
   *   videos: import('../common/api.js').MovieVideo[],
   *   backdrops: import('../common/api.js').MovieImage[],
   *   posters: import('../common/api.js').MovieImage[],
   *   emptyNote: string,
   * }}
   */
  let { title, videos, backdrops, posters, emptyNote } = $props()

  /** @type {'backdrops' | 'videos' | 'posters' | null} explicit tab choice */
  let mediaTab = $state(null)

  // Tabs for the galleries the item actually has — an empty one is left out
  // rather than rendered dead, so an item with no clips shows two tabs. Order
  // matters: the first surviving tab is the default, so backdrops lead.
  const mediaTabs = $derived(
    [
      { id: 'backdrops', label: 'Backdrops', count: backdrops.length },
      { id: 'videos', label: 'Videos', count: videos.length },
      { id: 'posters', label: 'Posters', count: posters.length },
    ].filter((t) => t.count > 0),
  )

  // Resolving the default lazily rather than seeding mediaTab in an $effect
  // keeps one source of truth, and self-corrects if the tab the user picked
  // vanishes because a refresh changed which galleries have entries.
  const activeTab = $derived(
    mediaTabs.some((t) => t.id === mediaTab) ? mediaTab : (mediaTabs[0]?.id ?? null),
  )

  /**
   * Switching tabs closes the lightbox — leaving it open would show an image
   * from a gallery no longer on screen.
   *
   * @param {string} id
   */
  function selectTab(id) {
    lightboxIndex = null
    mediaTab = /** @type {'backdrops' | 'videos' | 'posters'} */ (id)
  }

  const activeImages = $derived(
    activeTab === 'backdrops' ? backdrops : activeTab === 'posters' ? posters : [],
  )

  // ── Lightbox ─────────────────────────────────────────────────────────

  /** @type {number | null} index into activeImages, null when closed */
  let lightboxIndex = $state(null)
  /** @type {HTMLButtonElement | undefined} */
  let lightboxCloseEl = $state()
  /** @type {HTMLElement | null} the tile that opened it, refocused on close */
  let lightboxOpener = null

  const lightboxImage = $derived(
    lightboxIndex === null ? null : (activeImages[lightboxIndex] ?? null),
  )

  /**
   * @param {number} index
   * @param {MouseEvent} event
   */
  function openLightbox(index, event) {
    lightboxOpener = /** @type {HTMLElement} */ (event.currentTarget)
    lightboxIndex = index
  }

  function closeLightbox() {
    lightboxIndex = null
    // Returning focus to the tile is what makes keyboard browsing of the rail
    // survive an open/close round trip.
    lightboxOpener?.focus()
    lightboxOpener = null
  }

  /** @param {number} step */
  function stepLightbox(step) {
    if (lightboxIndex === null || activeImages.length === 0) return
    const n = activeImages.length
    lightboxIndex = (lightboxIndex + step + n) % n
  }

  /** @param {KeyboardEvent} event */
  function onLightboxKey(event) {
    if (lightboxIndex === null) return
    if (event.key === 'Escape') {
      event.preventDefault()
      closeLightbox()
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault()
      stepLightbox(-1)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      stepLightbox(1)
    }
  }

  // Move focus into the dialog on open, so Escape and the arrows reach the
  // handler above without the user tabbing first.
  $effect(() => {
    if (lightboxIndex !== null) lightboxCloseEl?.focus()
  })

  // The page behind a full-screen overlay must not scroll. Restoring the
  // previous value rather than clearing it keeps this safe if anything else
  // ever locks scrolling too.
  $effect(() => {
    if (lightboxIndex === null) return
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previous
    }
  })
</script>

{#if mediaTabs.length > 0}
  <div class="media-tabs" role="tablist" aria-label="Media">
    {#each mediaTabs as tab (tab.id)}
      <button
        type="button"
        role="tab"
        id="media-tab-{tab.id}"
        aria-selected={activeTab === tab.id}
        aria-controls="media-panel"
        class="media-tab"
        class:on={activeTab === tab.id}
        onclick={() => selectTab(tab.id)}
      >
        {tab.label}
        <span class="tab-count">{tab.count}</span>
      </button>
    {/each}
  </div>

  <div class="scroll-row" id="media-panel" role="tabpanel" aria-labelledby="media-tab-{activeTab}">
    {#if activeTab === 'videos'}
      {#each videos as vid (vid.url)}
        <!-- The one link in this section that leaves the app: TMDB lists the
             video, YouTube plays it. -->
        <a
          class="video-card"
          href={vid.url}
          target="_blank"
          rel="noopener noreferrer"
          style="--poster-hue: {hueFor(vid.name || title)}"
        >
          <div class="video-frame">
            {#if vid.thumb}
              <img src={assetUrl(vid.thumb)} alt="" loading="lazy" />
            {/if}
            <span class="video-play" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="20" height="20">
                <path d="M8 5.5v13l11-6.5z" fill="currentColor" />
              </svg>
            </span>
          </div>
          <p class="video-name" title={vid.name}>{vid.name || vid.type}</p>
          <p class="video-meta">
            <span class="video-type" class:official={vid.official}>{vid.type}</span>
            {#if vid.published_at}
              <span class="video-date">{formatVideoDate(vid.published_at)}</span>
            {/if}
          </p>
        </a>
      {/each}
    {:else}
      {#each activeImages as img, i (img.thumb)}
        <!-- aspect-ratio inline, because the same tile serves 16:9 backdrops
             and 2:3 posters and the shape is per-image. -->
        <button
          type="button"
          class="media-tile"
          style="aspect-ratio: {img.width} / {img.height}; --poster-hue: {hueFor(title)}"
          onclick={(e) => openLightbox(i, e)}
        >
          <img
            src={assetUrl(img.thumb)}
            srcset={activeTab === 'backdrops'
              ? backdropSrcset(img.thumb, img.full)
              : posterSrcset(img.thumb, img.full)}
            sizes="(min-width: 1100px) 250px, 200px"
            alt=""
            loading="lazy"
          />
        </button>
      {/each}
    {/if}
  </div>
{:else}
  <p class="row-note pending">{emptyNote}</p>
{/if}

{#if lightboxImage}
  <!-- Full-screen image viewer. Only the large locally cached variant is shown
       here; the rail already paid for the thumbs. -->
  <div class="lightbox" role="dialog" aria-modal="true" aria-label="{title} artwork">
    <!-- Click-outside closes. A button rather than a div so it is
         keyboard-reachable and needs no role juggling. -->
    <button type="button" class="lightbox-scrim" aria-label="Close" onclick={closeLightbox}
    ></button>

    <img class="lightbox-image" src={assetUrl(lightboxImage.full)} alt="" />

    <div class="lightbox-bar">
      {#if activeImages.length > 1}
        <button
          type="button"
          class="lightbox-btn"
          aria-label="Previous image"
          onclick={() => stepLightbox(-1)}
        >
          <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <path
              d="M15 5l-7 7 7 7"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
            />
          </svg>
        </button>
        <span class="lightbox-count">{(lightboxIndex ?? 0) + 1} / {activeImages.length}</span>
        <button
          type="button"
          class="lightbox-btn"
          aria-label="Next image"
          onclick={() => stepLightbox(1)}
        >
          <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <path
              d="M9 5l7 7-7 7"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
            />
          </svg>
        </button>
      {/if}
      <button
        type="button"
        class="lightbox-btn"
        aria-label="Close"
        bind:this={lightboxCloseEl}
        onclick={closeLightbox}
      >
        <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path
            d="M6 6l12 12M18 6L6 18"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
          />
        </svg>
      </button>
    </div>
  </div>
{/if}

<svelte:window onkeydown={onLightboxKey} />

<style>
  /* The tab strip scrolls rather than wraps: three pills fit on a phone, but
       the counts make them wider than a fixed row can promise. */
  .media-tabs {
    display: flex;
    gap: 0.4rem;
    margin-bottom: 0.7rem;
    overflow-x: auto;
    padding-bottom: 0.15rem;
  }

  .media-tab {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    flex-shrink: 0;
    padding: 0.35rem 0.7rem;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface);
    color: var(--text-dim);
    font: inherit;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    transition: border-color 0.15s var(--ease-lift);
  }

  .media-tab.on {
    border-color: var(--accent);
    background: var(--accent-tint);
    color: var(--text);
  }

  .tab-count {
    padding: 0 0.35rem;
    border-radius: 999px;
    background: rgb(255 255 255 / 0.08);
    color: var(--text-dim);
    font-size: 0.7rem;
    font-variant-numeric: tabular-nums;
  }

  .media-tab.on .tab-count {
    background: rgb(255 255 255 / 0.14);
    color: var(--text);
  }

  /* One rule for both galleries: the height is fixed so the rail is even, and
       the per-image aspect-ratio (set inline) decides the width. That is what
       lets 16:9 backdrops and 2:3 posters share a tile. */
  .media-tile {
    height: 112px;
    width: auto;
    flex-shrink: 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 18% 26%),
      hsl(var(--poster-hue) 22% 13%)
    );
    cursor: zoom-in;
    transition: transform 0.15s var(--ease-lift);
  }

  .media-tile:hover {
    transform: translateY(-2px);
  }

  .media-tile img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .video-card {
    width: 208px;
    flex-shrink: 0;
    color: inherit;
    text-decoration: none;
  }

  .video-frame {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-md);
    border: 1px solid var(--border);
    overflow: hidden;
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 18% 26%),
      hsl(var(--poster-hue) 22% 13%)
    );
  }

  .video-frame img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  /* The play badge sits over the frame, so it reads as a video whether or not
       the thumbnail loaded — a card with no frame shows the badge alone. */
  .video-play {
    position: absolute;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    padding-left: 2px;
    border-radius: 999px;
    background: rgb(0 0 0 / 0.55);
    color: #fff;
    transition: transform 0.15s var(--ease-lift);
  }

  .video-card:hover .video-play {
    transform: scale(1.12);
  }

  .video-name {
    margin: 0.45rem 0 0;
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .video-meta {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin: 0.15rem 0 0;
    font-size: 0.72rem;
    color: var(--text-dim);
  }

  .video-type {
    padding: 0.05rem 0.35rem;
    border-radius: var(--radius-sm);
    background: rgb(255 255 255 / 0.08);
  }

  /* Studio-published videos get the accent; a fan upload stays neutral. */
  .video-type.official {
    background: var(--accent-tint);
    color: var(--text);
  }

  .video-date {
    font-variant-numeric: tabular-nums;
  }

  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  /* A full-bleed button: it is the click-outside target, so it must cover the
       viewport and sit under the image and the controls. */
  .lightbox-scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: rgb(0 0 0 / 0.82);
    backdrop-filter: blur(6px);
    cursor: zoom-out;
  }

  .lightbox-image {
    position: relative;
    max-width: 92vw;
    max-height: 86vh;
    object-fit: contain;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lift);
    pointer-events: none;
  }

  .lightbox-bar {
    position: absolute;
    right: max(1rem, env(safe-area-inset-right));
    top: max(1rem, env(safe-area-inset-top));
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .lightbox-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    padding: 0;
    border: 1px solid rgb(255 255 255 / 0.14);
    border-radius: 999px;
    background: rgb(0 0 0 / 0.5);
    color: #fff;
    cursor: pointer;
  }

  .lightbox-btn:hover {
    background: rgb(0 0 0 / 0.75);
  }

  .lightbox-count {
    padding: 0 0.4rem;
    color: rgb(255 255 255 / 0.75);
    font-size: 0.75rem;
    font-variant-numeric: tabular-nums;
  }

  @media (min-width: 1100px) {
    /* One rule for both galleries: the height is fixed so the rail is even, and
         the per-image aspect-ratio (set inline) decides the width. That is what
         lets 16:9 backdrops and 2:3 posters share a tile. */
    .media-tile {
      height: 140px;
    }

    .video-card {
      width: 248px;
    }
  }

  @media (max-width: 599px) {
    /* A phone has no room to frame the image, so the lightbox fills it. */
    .lightbox-image {
      max-width: 100vw;
      max-height: 80vh;
      border-radius: 0;
    }

    .video-card {
      width: 172px;
    }

    /* One rule for both galleries: the height is fixed so the rail is even, and
         the per-image aspect-ratio (set inline) decides the width. That is what
         lets 16:9 backdrops and 2:3 posters share a tile. */
    .media-tile {
      height: 96px;
    }
  }

  @media (hover: none) {
    /* Hover lifts read as stuck state after a tap. */
    .media-tile:hover {
      transform: none;
    }

    .video-card:hover .video-play {
      transform: none;
    }

    /* Close/prev/next at the 44px touch minimum. */
    .lightbox-btn {
      width: 44px;
      height: 44px;
    }
  }
</style>
