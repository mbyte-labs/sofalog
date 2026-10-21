<script>
  // "You have dropped this show" — the banner the season and episode pages show
  // when the owning show's stored status is `dropped` (D-010).
  //
  // Read-only on purpose: drop and un-drop are show-page actions, and both
  // routes are id-addressed while these two pages hold only the show's tmdb_id.
  // So this states the fact and offers the way there, exactly as the episode
  // page's copies do for the season page.
  //
  // Nothing here can go stale: `dropped` is sticky, carried through every
  // recompute (D-010), so marking an episode from either page leaves it set.

  import { route as routeAction } from '@mateothegreat/svelte5-router'

  /**
   * @type {{
   *   showTitle: string,
   *   href: string,
   * }}
   */
  let { showTitle, href } = $props()
</script>

<!-- Deliberately not a live region: this is standing page content, and the
     whole page renders in the same frame it does, so announcing it separately
     would talk over that. -->
<p class="drop-notice">
  <span class="drop-mark" aria-hidden="true">⚠</span>
  <span>
    You've dropped <strong>{showTitle}</strong>. Marking episodes still works —
    <a {href} use:routeAction>resume it on the show page</a>.
  </span>
</p>

<style>
  /* The SPA's one banner shape (UpNextPage's .notice), with the red tint
     instead of --surface behind it: a dropped show is a state wearing the
     colour of its pill on the show page, not a transient error. */
  .drop-notice {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    margin: 0 0 1.25rem;
    padding: 0.6rem 0.9rem;
    background: var(--err-tint);
    border: 1px solid var(--err);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 0.85rem;
  }

  .drop-mark {
    flex-shrink: 0;
    color: var(--err);
  }

  .drop-notice a {
    color: var(--err);
    text-decoration: underline;
  }

  .drop-notice a:hover {
    color: var(--text);
  }
</style>
