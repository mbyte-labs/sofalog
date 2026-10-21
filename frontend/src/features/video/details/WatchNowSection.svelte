<script>
  // The "Watch now" section: copies the user owns, and where the item streams.
  //
  // The copies half is real (D-021): the tiles render the target's stored
  // kinds, and tapping one hands the choice to the page via `onselect` — the
  // page owns the confirmation dialog and the API call, exactly as it owns
  // WatchLogDialog's. Two shapes:
  //
  //   - `copies` (movie and season pages): the target's own kinds; a tile is
  //     simply owned or not.
  //   - `seasonCopies` (show page): one kinds-array per NON-SPECIAL season. A
  //     show's copies live per-season, so a tile can be fully, partly or not
  //     covered, and it says so ("3 of 5 seasons").
  //
  // `readonly` (episode page) is the same single shape borrowed rather than
  // owned: an episode is never itself owned, so its page shows its SEASON's
  // copies and the tiles open an explanation instead of a write. They stay
  // buttons — they do open a dialog — so every hover, focus and touch-target
  // rule below keeps applying unchanged.
  //
  // `managedKinds` names the kinds a server-side scan owns rather than the
  // user (D-033: the Jellyfin library scan, when a server is configured).
  // Such a tile is read-only on every page — no aria-pressed, a "synced"
  // caption — and still opens a dialog, which the page fills with an
  // explanation instead of a write. The state itself is the server's answer,
  // read out of the same `copies` array as the rest.
  //
  // `incompleteKinds` names the lit kinds that do not hold EVERYTHING: a
  // season the server has some episodes of, a show with a season absent or
  // partial. Such a tile wears amber instead of the accent, so "on Jellyfin"
  // never quietly means "some of it". Only the page can decide it — it knows
  // the aired episodes — so it arrives as a prop like the rest.
  //
  // The streaming half is TMDB's watch-provider data for one country (D-022),
  // handed in whole: the server picks the region, folds a service listed under
  // several offers into one entry and proxies the logos, so there is nothing
  // to derive here.
  //
  // Its three states are the server's, not this component's to invent:
  // `streaming` null means nobody looked (a season of an untracked show, or a
  // snapshot with no listing yet) and the sub-group is hidden — silence is the
  // only honest rendering of "no answer". An empty `providers` means TMDB DID
  // answer and named nobody, which is a fact worth stating rather than
  // vanishing over. Anything else is tiles.

  import { assetUrl } from '../../../lib/images.js'
  import { hueFor, initialsFor } from '../common/posterPlaceholder.js'
  import { JELLYFIN_PATH } from '../../../lib/brandMarks.js'
  import BrandIcon from '../common/components/BrandIcon.svelte'

  /**
   * `streaming` is the server's whole listing, or null when the view has none
   * to show — it is data, not a toggle, so no page decides whether to render
   * the sub-group.
   *
   * `subject` names WHOSE listing this is, not which page it is on: TMDB
   * publishes providers per movie and per show only, so the season and episode
   * pages pass 'show' — they are showing the show's answer, and "this season
   * isn't streaming anywhere" would imply a per-season answer that does not
   * exist.
   *
   * @type {{
   *   copies?: string[],
   *   seasonCopies?: string[][] | null,
   *   streaming?: import('../common/api.js').Streaming | null,
   *   subject?: 'movie' | 'show',
   *   readonly?: boolean,
   *   managedKinds?: string[],
   *   incompleteKinds?: string[],
   *   disabled?: boolean,
   *   onselect?: (src: { id: string, label: string },
   *     state: { owned: boolean, have: number, total: number }) => void,
   * }}
   */
  let {
    copies = [],
    seasonCopies = null,
    streaming = null,
    subject = 'show',
    readonly = false,
    managedKinds = [],
    incompleteKinds = [],
    disabled = false,
    onselect,
  } = $props()

  /** @param {string} id */
  const isManaged = (id) => managedKinds.includes(id)
  /** @param {string} id */
  const isIncomplete = (id) => incompleteKinds.includes(id)

  // Ownership sources — the client copy of the server's kind vocabulary.
  // "Other" covers copies with no branded format: a disc-less physical medium
  // or a bare file.
  const OWN_SOURCES = [
    { id: 'jellyfin', label: 'Jellyfin' },
    { id: 'dvd', label: 'DVD' },
    { id: 'bluray', label: 'Blu-ray' },
    { id: 'uhd', label: '4K Blu-ray' },
    { id: 'physical', label: 'Other Physical' },
    { id: 'digital', label: 'Other Digital' },
  ]

  /**
   * One tile's state. In show shape, `have`/`total` count seasons; in single
   * shape they stay 0/0 and only `owned` matters. Derived per render — the
   * props are the source of truth, never local toggles.
   *
   * @param {string} id
   * @returns {{ owned: boolean, have: number, total: number }}
   */
  function stateFor(id) {
    if (seasonCopies != null) {
      const have = seasonCopies.filter((kinds) => kinds.includes(id)).length
      return { owned: have > 0, have, total: seasonCopies.length }
    }
    return { owned: copies.includes(id), have: 0, total: 0 }
  }

  /**
   * The coverage caption under a show-page tile — the answer to "how many
   * seasons are on DVD". Empty in single shape and for an unowned kind.
   *
   * @param {{ owned: boolean, have: number, total: number }} s
   */
  function coverageLabel(s) {
    if (seasonCopies == null || !s.owned) return null
    if (s.have === s.total) return `All ${s.total} season${s.total === 1 ? '' : 's'}`
    return `${s.have} of ${s.total} seasons`
  }

  /**
   * @param {{ id: string, label: string }} src
   * @param {{ owned: boolean, have: number, total: number }} s
   */
  function tileTitle(src, s) {
    const label = coverageLabel(s)
    const state = label
      ? `${s.owned ? 'On' : 'Not on'} ${src.label} — ${label.toLowerCase()}`
      : s.owned
        ? `On ${src.label}`
        : `Not on ${src.label}`
    if (isManaged(src.id)) {
      const gap = isIncomplete(src.id) ? ', episodes missing' : ''
      return `${state}${gap} — synced from your ${src.label} server`
    }
    const owned = state.replace(/^On /, 'Owned on ').replace(/^Not on /, 'Not owned on ')
    return readonly ? `${owned} — managed on the season page` : owned
  }

  // The five offers the API publishes, as the caption under a tile. "Free" and
  // "Free with ads" stay apart because the second one costs the viewer
  // something the first does not.
  const OFFER_LABELS = {
    stream: 'Stream',
    free: 'Free',
    ads: 'Free with ads',
    rent: 'Rent',
    buy: 'Buy',
  }

  // Disc-format logos — traced from the public-domain text-logo SVGs on
  // Wikimedia Commons (DVD_logo.svg, Blu-ray_Disc.svg; both below the threshold of originality). Coordinates rounded; each keeps
  // its source viewBox. Blu-ray/UHD keep their brand colors, DVD inherits.
  const DVD_LOGO = {
    viewBox: '0 0 1058.4 465.84',
    paths: [
      {
        fill: 'currentColor',
        d: 'm91.1 0-13.7 57.7 102.3 0h24c65.7 0 105.9 26.4 94.7 73.4-12.1 51.1-69.6 73.4-130.7 73.4h-22.9l29.8-125.5h-102.3l-43.5 183.2h145.1c109.1 0 212.8-57.6 231-131.2 3.3-13.5 2.9-47.3-5.4-67.4-0.2-0.8-0.4-1.4-1.2-3-0.3-0.7-0.6-3.6 1.1-4.3 0.9-0.4 2.7 1.5 2.9 2 0.9 2.2 1.5 3.9 1.5 3.9l92.3 260.6 235-265.2 99.5-0.1h24c65.8 0 106.2 26.4 95.1 73.4-12.1 51.1-69.9 73.4-131 73.4h-23l29.8-125.5h-102.3l-43.5 183.2h145.1c109 0 213.5-57.4 231-131.2 17.5-73.8-59.1-131.2-168.7-131.2h-216.4s-57.3 67.9-68 80.7c-57.1 68.8-67.2 87.2-69 92 0.2-4.8-1.8-23.4-26.2-93-6.5-18.5-27.4-79.7-27.4-79.7h-389.2zm408.8 324.2c-276 0-499.8 31.7-499.8 70.8s223.8 70.8 499.8 70.8c276 0 499.8-31.7 499.8-70.8s-223.8-70.8-499.8-70.8zm-18.1 48.6c63 0 114.1 10.6 114.1 23.6s-51.1 23.6-114.1 23.6c-63 0-114.1-10.6-114.1-23.6s51.1-23.6 114.1-23.6z',
      },
      {
        fill: 'currentColor',
        d: 'm963.6 445.1-0.7 5.2h13.1l-5.1 36.3h5.7l5.1-36.3h11.7l0.7-5.2h-30.5zm41.2 0-13.7 41.5h5.5l10.2-31.8h0.1l7.2 31.8 16.7-31.5h0.1v31.5h5.5v-41.5h-5.5l-14.9 28.1-6.4-28.1h-4.7z',
      },
    ],
  }
  const BLURAY_LOGO = {
    viewBox: '-0.744 82.256 386 207',
    paths: [
      {
        fill: '#0095d5',
        d: 'M91.6,237.7c-0.7,0-3.1,0.7-5.1,3.7c-2.1,3.1-11.5,18.6-13.2,21.5c-1.8,2.9-1.2,4.2,0.7,4.2c1.7,0,2.1,0,4.2,0c1.9,0,3.7-2.2,4.4-3.7c1-1.4,12.1-19.4,13.9-22.1c1.5-2.6,0.6-3.6-0.7-3.6C94.6,237.7,91.6,237.7,91.6,237.7L91.6,237.7z M53.7,267.1c7.1-0.3,11.8-0.6,17.3-10.7c2.4-4.6-4.3-4.7-4.3-4.7c-1,0,6.1,0.1,9.9-7.8c3.5-7.2-2.6-6.7-3.7-6.8c-5.3-0.7-15.1-0.4-22.6,0c-1.5,0-3.3,2.1-4.3,3.3c-0.7,1.4-12.2,20-13.9,22.6c-1.8,2.8,0,3.9,1.8,4C39.1,267.4,45.8,267.4,53.7,267.1L53.7,267.1L53.7,267.1z M59.8,257.4c-1.9,4-5.6,4.2-7.6,4.2c-1.9,0-4.9,0-5.6,0s-1.4,0-0.7-1.5c1-1.2,2.4-3.5,2.9-4.3c0.6-1,1.1-1.2,2.4-1.2c0,0,4.7,0,6,0C58.3,254.5,61.2,254.4,59.8,257.4L59.8,257.4L59.8,257.4z M64.9,245.5c-1.9,4-5.7,4-7.6,4c-2.1,0-2.5,0-3.3,0c-0.6,0-1.5,0-0.6-1.2c1-1.4,2.1-3.7,2.8-4.6c0.7-0.8,1.2-1.2,2.2-1.2c0,0,2.9,0,4,0C63.6,242.4,66.5,242.4,64.9,245.5L64.9,245.5z M284.8,248.8l-9.2,14.6c-1.9,3.1-0.3,3.7,0.7,3.7c1,0,2.4,0,4,0c1.5,0,2.6-0.1,5-3.7l9.2-14.8c2.5-4-0.6-3.7-2.5-3.7C288.5,244.9,287.7,244.8,284.8,248.8L284.8,248.8z M322.5,249.8h-11c-1.1,0-2.6,1-3.5,2.1c-0.7,1.1-0.3,2.2,0.8,2.2c0-0.1,4.4,0,7.8,0c3.5-0.1,4.7,2.2,1,7.4c-0.7,1.1-2.2,2.9-3.5,3.6c0,0-3.1,2.5-12.9,2.5c-9,0-9.4-1-9.4-1c-0.8-0.7-1.1-1.9-0.8-2.6c0.4-0.6,1.8-1.1,2.9-1.1H305.3c1.1,0,2.6-1,3.3-2.2c0.8-1.1,0.4-2.5-0.7-2.5c0,0-4.4,0-8.5,0c-4,0-3.5-2.6-0.4-7.1c0.8-1.2,2.4-2.8,3.5-3.6c0,0,3.3-2.5,13-2.5c3.9,0,10.4-0.4,10,2.9C325.4,249.4,323.2,249.8,322.5,249.8L322.5,249.8z M351.2,249.9h-8.9c-1.1,0-2.6,1-3.3,2.4l-5.3,8.2c-0.7,1.1-0.3,2.4,0.6,2.4c0,0,9,0,10.5,0c1.5,0,2.5,0.1,2.4,0.6c-0.1,1.5-2.4,4.2-14.7,4.2c-11.1,0-10.4-1.5-10.3-3.2c0.3-1.8,5.7-10.4,7.4-13c1.7-2.8,4-6.4,15-6.4c10.3,0,9.7,2.4,9.6,2.9C354.1,248.4,353.6,249.9,351.2,249.9L351.2,249.9z M299.8,236.5c0.8,0,1.8,0.8,1.1,2.2c-0.8,1.1-1.1,1.7-1.5,2.5c-0.6,0.8-1.8,1.2-3.3,1.2c-1.5,0-5.1,0-5.7,0c-1.1,0-1.1-1-0.6-1.9c0.4-0.8,0.7-1.2,1.1-1.8c0.4-0.7,1.1-2.2,3.3-2.2H299.8L299.8,236.5z M133.8,252.4c-2.1,0-2.9,1.4-3.3,2.1c-0.3,0.6-0.7,1-1.2,1.9c-0.4,1-0.3,1.9,0.3,1.9c0.7,0,4.4,0,6,0c1.8,0,2.8-0.6,3.3-1.4c0.6-1,0.7-1.2,1.5-2.6c1-1.4-0.3-1.9-1-1.9L133.8,252.4L133.8,252.4z M271.7,258.1c3.6-5.8,1.4-2.2,6.5-10.4c5-8.3-2.9-10-10.7-10c-11.9,0-15.5,0.6-15.5,0.6c-1.2,0-2.9,1.1-3.6,2.5l-14.4,23.3c-0.8,1.4-0.6,2.5,0.6,2.8c0,0,3.5,0.3,15.4,0.3C266.3,267.1,267.8,264.1,271.7,258.1L271.7,258.1L271.7,258.1zM266.9,247.7c0,0-6.2,9.9-6.7,10.4c-0.6,0.8-2.5,3.5-5.4,3.5c-1.8,0-4.3,0-6.1,0c-1.1,0-1.7-0.1-1-1.2l10-16c0.4-0.6,1.1-1,1.9-1c1.7,0,4.7,0,6,0C267.6,243.4,268.1,245.5,266.9,247.7L266.9,247.7z M176.1,253.5c-3.5,0-4.6,0.3-7.6,0.4c-1.2,0-2.9,1-3.6,2.4l-2.9,5c-3.6,5.7,4.2,6.1,10.3,6.1c9.9,0,14.6-2.8,14.6-2.8c1.2-0.6,2.8-2.1,3.5-3.3l6.4-10.4c0.7-1.1,1-3.1,0.3-4.2c0,0-0.8-1.9-10.7-1.9c-9.7,0-13.6,1.5-13.6,1.5c-1.1,0.6-2.5,1.5-2.9,2.1c-0.3,0.8,0.3,1.4,1.4,1.4h14.4c1.1,0,1.5,1,1,1.9c-1.2,1.7-3.3,1.8-3.3,1.8H176.1L176.1,253.5z M181.7,260.3v0.1c-1,1.4-2.9,2.6-4,2.6h-2.9h-2.6c-1.1,0-1.5-1.2-0.6-2.6v-0.1c1-1.5,2.9-2.8,4-2.8h5.6C182.2,257.6,182.6,258.8,181.7,260.3L181.7,260.3z M157,251.9c1-1.4,2.5-2.4,3.6-2.4H165c0.7,0,2.9-0.1,3.1-1.5c0.1-2.2-4.6-2.8-6.4-2.8c-6.5,0-10.1,0.3-13.9,5.8c-2.9,4.4-5.8,9-7.8,12.3c-1.7,3.1-0.1,3.7,0.8,3.7c1,0,2.5,0,4,0s2.6-0.1,5-3.7L157,251.9L157,251.9z M127.9,249l-6.2,10.4c-0.8,1.1,0,1.7,0.7,1.7c0.8,0,1.5,0,2.2,0c0.7,0,1.7,0.8,0.8,2.1c-0.7,1.4-0.8,1.9-1.5,2.8c-0.6,1-1.5,1.2-3.2,1.2c-1.1,0-1.9,0-2.6,0c-1.8,0-0.8-2.6-0.8-2.6c-2.6,2.9-8.7,2.9-15.1,2.9c-6.1,0-14,0-10.4-6.1l7.8-12.5c1.2-2.1,2.2-2.9,3.5-3.5c0,0,0.3-0.4,3.6-0.4c2.2,0,5.1,0,2.6,3.9l-7.1,11.5c-0.8,1.1-0.4,2.2,0.8,2.2h4.9c1.2,0,2.8-1.1,3.7-2.2c0,0,3.6-6.2,6.8-11.4c0.8-1.2,2.9-4,5-4c0,0,1.9,0,3.5,0C128.4,244.9,130.2,245.4,127.9,249L127.9,249z M191.1,273h11.5l2.4,0.1c1.2,0,2.8-1.1,3.5-2.5l2.4-3.5c-1.5,0.3-3.1,0.3-4.9,0.3c-5.8,0-13.9,0-10.3-6.1l7.5-12.1c1-1.4,2.5-2.9,3.5-3.5c0-0.3,0.6-0.4,3.7-0.4c2.1,0,5.4-0.6,2.5,3.9l-6.7,11.1c-0.8,1.1-0.6,2.2,0.6,2.2h4.7c1,0,2.8-1.1,3.5-2.2l6.9-11.1c0.7-1.4,2.4-2.9,3.5-3.5c0,0,0.6-0.4,3.7-0.4c1.8,0,5.4-0.6,2.5,3.9l-5.8,9.3c-0.3,0.4-0.4,0.6-0.7,1.2l-7.4,11.9c-0.8,1.2-2.4,2.8-3.5,3.5c0,0-4.9,2.8-14.6,2.8c-0.4,0-1,0-1.4,0c-9-0.3-8.6-0.8-9.2-1.2c-0.6-0.1-1.1-1-1-2.1C188.3,273.7,189.6,273,191.1,273L191.1,273z M99.5,117.4c-0.3,0-0.3,0.1-0.4,0.3c-12.1,16.9-19.1,28.2-26.6,42l-0.8,1.5l-0.1,0.6c-0.7,1.4-0.6,2.9,0.4,4.2c4.4,6.9,29.3,15.1,84.7,15.1c41.4,0,85.3-6.7,85.3-19.1c0-11.8-43.3-18.9-85.3-18.9c-14,0-27.8,1.8-31.6,2.5c3.7-5.8,19.6-27.1,19.6-27.2c0.3-0.3,0.3-0.3,0.3-0.4v-0.3c-0.3-0.1-0.4-0.3-0.7-0.3L99.5,117.4L99.5,117.4zM115.6,161.9c0-2.5,15.7-6.1,40.9-6.1c25.3,0,40.8,3.6,40.8,6.1c0,2.8-15.5,6.2-40.8,6.2C131.3,168.2,115.6,164.7,115.6,161.9L115.6,161.9z M130.9,224.3c7.1,0.1,206.2,7.1,210.7-63.6c3.6-58.4-142.9-52.9-142.9-52.9c-0.3,0-1.2,0-1.2,0.7c0,0.8,0.4,1,1,1c40.7,0,106.7,16.2,104.6,51.2c-1.7,28.3-53.3,61.6-172.1,61.6c-0.6,0-1.1,0.6-1.1,1C129.8,223.7,130.2,224.1,130.9,224.3L130.9,224.3z',
      },
    ],
  }
</script>

<!-- The Jellyfin brand gradient, referenced as fill="url(#jellyfin-grad)".
     It ships with the markup that uses it, so the section stays portable. -->
<svg width="0" height="0" class="svg-defs" aria-hidden="true">
  <defs>
    <linearGradient id="jellyfin-grad" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#aa5cc3" />
      <stop offset="1" stop-color="#00a4dc" />
    </linearGradient>
  </defs>
</svg>

<!-- A traced disc-format wordmark at its native aspect ratio; the tile's CSS
     letterboxes it into the logo box (SVG's default xMidYMid meet). -->
{#snippet formatLogo(
  /** @type {{ viewBox: string, paths: { fill: string, d: string, transform?: string }[] }} */ logo,
)}
  <svg viewBox={logo.viewBox} aria-hidden="true">
    {#each logo.paths as p, i (i)}
      <path d={p.d} fill={p.fill} transform={p.transform} />
    {/each}
  </svg>
{/snippet}

<!-- Icon per ownership source. Jellyfin and the disc formats are their real
     marks; the two "Other" archives get hand-drawn glyphs (box for physical,
     drive for digital). All decorative — the tile's label names the source. -->
{#snippet ownIcon(/** @type {string} */ id)}
  {#if id === 'jellyfin'}
    <BrandIcon path={JELLYFIN_PATH} size={24} fill="url(#jellyfin-grad)" />
  {:else if id === 'dvd'}
    {@render formatLogo(DVD_LOGO)}
  {:else if id === 'bluray'}
    {@render formatLogo(BLURAY_LOGO)}
  {:else if id === 'uhd'}
    <!-- The real UHD wordmark is unreadable this small; a plain resolution
         badge is the recognizable mark. -->
    <span class="uhd-mark">4K</span>
  {:else if id === 'physical'}
    <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <rect
        x="3.5"
        y="4.5"
        width="17"
        height="4.2"
        rx="1"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
      />
      <path
        d="M5.2 8.7v9.6a1.2 1.2 0 0 0 1.2 1.2h11.2a1.2 1.2 0 0 0 1.2-1.2V8.7"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
      />
      <path d="M9.8 12.6h4.4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </svg>
  {:else}
    <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
      <rect
        x="3.5"
        y="7"
        width="17"
        height="10"
        rx="2"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
      />
      <path d="M7.2 12h5.6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
      <circle cx="16.6" cy="12" r="1.2" fill="currentColor" />
    </svg>
  {/if}
{/snippet}

<!-- Two ways to watch: copies the user owns and online services (TMDB's
     watch-provider data for the configured region — when there is none, the
     sub-group vanishes, no empty state). -->
<div class="section s-watch">
  <h2 class="section-label">Watch now</h2>
  <div class="watch-now">
    <div class="watch-group">
      <h3 class="watch-sublabel">{readonly ? 'Your copies of this season' : 'Your copies'}</h3>
      <div class="own-tiles" role="group" aria-label="Owned sources">
        {#each OWN_SOURCES as src (src.id)}
          {@const s = stateFor(src.id)}
          <!-- aria-pressed is three-state on the show page: a partly covered
               kind is honestly "mixed", not on or off. A read-only or a
               scan-managed tile gets none at all — it toggles nothing. Every
               tile on every page opens a dialog, so aria-haspopup is
               unconditional. -->
          <button
            class="own-tile"
            class:owned={s.owned}
            class:partial={s.owned && s.total > 0 && s.have < s.total}
            class:managed={isManaged(src.id)}
            class:incomplete={s.owned && isIncomplete(src.id)}
            aria-pressed={readonly || isManaged(src.id)
              ? undefined
              : s.owned
                ? s.total > 0 && s.have < s.total
                  ? 'mixed'
                  : 'true'
                : 'false'}
            aria-haspopup="dialog"
            title={tileTitle(src, s)}
            {disabled}
            onclick={() => onselect?.(src, s)}
          >
            <span class="own-logo">{@render ownIcon(src.id)}</span>
            <span class="own-name">{src.label}</span>
            {#if coverageLabel(s)}
              <span class="own-count">{coverageLabel(s)}</span>
            {:else if isManaged(src.id)}
              <!-- The scan's tile says where its answer comes from; on the
                   show page the coverage caption already fills the slot. An
                   incomplete season says so instead. -->
              <span class="own-count"
                >{s.owned && isIncomplete(src.id) ? 'incomplete' : 'synced'}</span
              >
            {/if}
          </button>
        {/each}
      </div>
    </div>

    <!-- Rendered whenever there IS an answer, empty or not — only a null
         listing (nobody looked) stays silent. -->
    {#if streaming}
      <div class="watch-group">
        <h3 class="watch-sublabel">Streaming</h3>
        {#if streaming.providers.length === 0}
          <!-- The pages' own empty-state idiom; both rules are global, under
               .detail, which is this component's ancestor everywhere. -->
          <p class="row-note pending">
            This {subject} isn't on any streaming service in {streaming.region}.
          </p>
        {:else}
          <div class="stream-tiles">
            <!-- Keyed by provider_id, not name: two services can share a display
               name across regions, and the id is the stable one. -->
            {#each streaming.providers as p (p.provider_id)}
              <div class="stream-tile">
                <span class="stream-logo" style="--poster-hue: {hueFor(p.name)}" aria-hidden="true">
                  {#if p.logo}
                    <!-- Locally proxied, never a provider URL. -->
                    <img src={assetUrl(p.logo)} alt="" loading="lazy" />
                  {:else}
                    {initialsFor(p.name)}
                  {/if}
                </span>
                <span class="stream-name" title={p.name}>{p.name}</span>
                <span class="stream-kind">{OFFER_LABELS[p.offer] ?? p.offer}</span>
              </div>
            {/each}
          </div>
        {/if}
        <!-- Kept in both arms: with nothing listed, "where did this answer come
             from" matters more, not less. The region explains the list — a
             service missing from it is missing in THIS country, not missing
             outright. The JustWatch credit is the one provider disclaimer that
             lives off Settings → Data Sources — TMDB's terms ask for it beside
             the data. -->
        <p class="fine-print">
          Availability in {streaming.region}. Powered by
          {#if streaming.link}
            <a href={streaming.link} target="_blank" rel="noopener noreferrer">JustWatch</a>
          {:else}
            JustWatch
          {/if}, via TMDB.
        </p>
      </div>
    {/if}
  </div>
</div>

<style>
  .watch-now {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
  }

  /* The gap does all spacing inside a group — .watch-now p wins the margin
       specificity contest against .demo-hint / .fine-print, deliberately. */
  .watch-now p {
    margin: 0;
  }

  .watch-group {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.6rem;
  }

  /* Sub-label: same voice as .section-label, one step quieter. */
  .watch-sublabel {
    margin: 0;
    color: var(--text-dim);
    opacity: 0.7;
    font-size: 0.66rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  .own-tiles {
    display: flex;
    flex-wrap: wrap;
    gap: 0.85rem 1rem;
  }

  /* Same anatomy as .stream-tile — logo box, name underneath — but a real
       toggle button. State lives on the logo box: accent means owned, the off
       hover only brightens. */
  .own-tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    /* A step wider than .stream-tile so "Other Physical" fits unclipped. */
    width: 5.4rem;
    padding: 0;
    border: none;
    background: none;
    color: var(--text-dim);
    font: inherit;
    cursor: pointer;
  }

  .own-logo {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.6rem;
    height: 2.6rem;
    border-radius: var(--radius-sm);
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text);
    transition:
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  /* The wordmarks are wide; letterbox them inside the square box. Overrides
       the hand-drawn glyphs' attribute sizes too, so every mark fills alike. */
  .own-logo svg {
    width: 86%;
    height: 74%;
  }

  /* Text stand-in for the 4K tile, colored like the other currentColor marks. */
  .uhd-mark {
    font-size: 1.05rem;
    font-weight: 800;
    letter-spacing: 0.02em;
    line-height: 1;
  }

  .own-name {
    margin-top: 0.4rem;
    max-width: 100%;
    font-size: 0.72rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    transition: color var(--ease-lift);
  }

  /* The show page's per-kind coverage ("3 of 5 seasons"). */
  .own-count {
    margin-top: 0.1rem;
    max-width: 100%;
    color: var(--text-dim);
    font-size: 0.65rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .own-tile:hover:enabled .own-logo {
    border-color: var(--text-dim);
  }

  .own-tile:hover:enabled .own-name {
    color: var(--text);
  }

  .own-tile.owned .own-logo {
    background: var(--accent-tint);
    border-color: var(--accent);
  }

  .own-tile.owned:hover:enabled .own-logo {
    background: var(--accent-tint-strong);
  }

  .own-tile.owned .own-name {
    color: var(--text);
  }

  /* Partly covered (show page): the accent border says "some", the missing
     tint says "not all" — the caption underneath carries the exact figure. */
  .own-tile.partial .own-logo {
    background: var(--surface);
  }

  .own-tile.partial:hover:enabled .own-logo {
    background: var(--accent-tint);
  }

  /* Off state mutes the mark — brand colors and gradients go grayscale. */
  .own-tile:not(.owned) .own-logo {
    filter: grayscale(1);
    opacity: 0.55;
    transition:
      filter var(--ease-lift),
      opacity var(--ease-lift),
      background var(--ease-lift),
      border-color var(--ease-lift);
  }

  /* Lit, but not all of it (D-033): amber in place of the accent, so a
     season with episodes missing — or a show with a season absent or partial
     — never reads the same as a complete one. After .owned and .partial on
     purpose, because it overrides both. */
  .own-tile.incomplete .own-logo,
  .own-tile.incomplete:hover:enabled .own-logo {
    background: var(--warn-tint-strong);
    border-color: var(--warn);
  }

  /* A scan-managed tile (D-033) opens an explanation, not a write, so its
     hover does not promise a toggle: the border stays put. */
  .own-tile.managed:hover:enabled .own-logo {
    border-color: var(--border);
  }

  .own-tile.managed.owned:hover:enabled .own-logo {
    background: var(--accent-tint);
    border-color: var(--accent);
  }

  /* Disabled means "a write is in flight" — the shared .watch-cta treatment. */
  .own-tile:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .own-tile:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-sm);
  }

  /* The 2.6rem logo box alone is under the 44px touch floor; an invisible
     halo takes the tap target past it without growing the visual (the
     episode-mark badge's pattern). The tile is the positioning context. */
  @media (hover: none) {
    .own-tile {
      position: relative;
    }

    .own-tile::after {
      content: '';
      position: absolute;
      inset: -0.35rem;
    }

    .own-tile:hover:enabled .own-logo {
      border-color: var(--border);
    }

    .own-tile:hover:enabled .own-name {
      color: var(--text-dim);
    }

    .own-tile.owned:hover:enabled .own-name {
      color: var(--text);
    }

    .own-tile.owned:hover:enabled .own-logo {
      background: var(--accent-tint);
      border-color: var(--accent);
    }

    .own-tile.partial:hover:enabled .own-logo {
      background: var(--surface);
    }
  }

  .stream-tiles {
    display: flex;
    flex-wrap: wrap;
    gap: 0.85rem 1rem;
  }

  .stream-tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 4.6rem;
  }

  /* Holds the proxied provider mark, or — when TMDB has no logo for the
       service — a name monogram in the poster placeholder's hue/initials
       language. overflow clips the image to the box's radius. */
  .stream-logo {
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 2.6rem;
    height: 2.6rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: linear-gradient(
      160deg,
      hsl(var(--poster-hue) 40% 32%),
      hsl(var(--poster-hue) 45% 14%)
    );
    color: rgb(255 255 255 / 0.75);
    font-size: 0.8rem;
    font-weight: 700;
    letter-spacing: 0.03em;
  }

  /* The marks are square-ish but not uniformly so; cover keeps every tile the
     same size rather than letting a wide wordmark set the row height. */
  .stream-logo img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .stream-name {
    margin-top: 0.4rem;
    max-width: 100%;
    font-size: 0.72rem;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .stream-kind {
    color: var(--text-dim);
    font-size: 0.65rem;
  }

  .fine-print {
    color: var(--text-dim);
    font-size: 0.72rem;
  }

  /* The one outbound link in this section; the attribution has to read as one
     sentence, so it stays inline and only picks up the accent. */
  .fine-print a {
    color: inherit;
    text-decoration: underline;
  }

  .fine-print a:hover {
    color: var(--accent);
  }

  @media (min-width: 1100px) {
    /* The two watch groups share one row, echoing the .details divider; when
           streaming is hidden, :not(:first-child) leaves no orphan border. */
    .watch-now {
      flex-direction: row;
      align-items: flex-start;
      gap: 2rem;
    }

    .watch-group:not(:first-child) {
      padding-left: 2rem;
      border-left: 1px solid var(--border);
    }
  }
</style>
