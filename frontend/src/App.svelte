<script>
  import { Router, route, StatusCode } from '@mateothegreat/svelte5-router'
  import Home from './Home.svelte'
  import NotFound from './NotFound.svelte'
  import ComingSoon from './ComingSoon.svelte'
  import UpNextPage from './features/video/upnext/UpNextPage.svelte'
  import UpcomingPage from './features/video/upcoming/UpcomingPage.svelte'
  import HistoryPage from './features/video/history/HistoryPage.svelte'
  import WatchlistPage from './features/video/lists/WatchlistPage.svelte'
  import FavoritesPage from './features/video/lists/FavoritesPage.svelte'
  import ListsPage from './features/video/lists/ListsPage.svelte'
  import DroppedPage from './features/video/lists/DroppedPage.svelte'
  import SearchPage from './features/video/search/SearchPage.svelte'
  import TrendingPage from './features/video/trending/TrendingPage.svelte'
  import StatsPage from './features/video/stats/StatsPage.svelte'
  import MovieDetailsPage from './features/video/details/MovieDetailsPage.svelte'
  import ShowDetailsPage from './features/video/details/ShowDetailsPage.svelte'
  import SeasonDetailsPage from './features/video/details/SeasonDetailsPage.svelte'
  import EpisodeDetailsPage from './features/video/details/EpisodeDetailsPage.svelte'
  import PersonDetailsPage from './features/video/people/PersonDetailsPage.svelte'
  import SettingsPage from './features/settings/SettingsPage.svelte'
  import DataSourcesPage from './features/settings/DataSourcesPage.svelte'
  import RestorePage from './features/settings/RestorePage.svelte'
  import TicketBoxRestorePage from './features/settings/TicketBoxRestorePage.svelte'
  import TMDBAccountPage from './features/settings/TMDBAccountPage.svelte'
  import JellyfinPage from './features/settings/JellyfinPage.svelte'
  import LoginPage from './features/auth/LoginPage.svelte'
  import SetupPage from './features/auth/SetupPage.svelte'
  import UsersPage from './features/auth/UsersPage.svelte'
  import AccountPage from './features/auth/AccountPage.svelte'
  import { displayLabel, getSessionStatus } from './features/auth/api.js'
  import { session, applyStatus, markSessionError } from './features/auth/session.svelte.js'

  // Feature routes (auth, video, games) get added here as their
  // milestones land; each maps a path prefix to a component from src/features/.
  const routes = [
    { path: '/', component: Home },
    // Generic prefixes BEFORE specific paths: the router keeps the LAST
    // successful match, and 'video' base-matches '/video/history', so the
    // exact 'video/history' entry must come later to win.
    { path: 'video', component: UpNextPage },
    { path: 'video/upcoming', component: UpcomingPage },
    { path: 'video/history', component: HistoryPage },
    { path: 'video/search', component: SearchPage },
    // The Lists hub before its lists — same prefix-before-path rule.
    { path: 'video/lists', component: ListsPage },
    { path: 'video/lists/watchlist', component: WatchlistPage },
    { path: 'video/lists/favorites', component: FavoritesPage },
    { path: 'video/lists/dropped', component: DroppedPage },
    { path: 'video/trending', component: TrendingPage },
    // The user's own numbers over one period (D-039).
    { path: 'video/stats', component: StatsPage },
    // The movie id travels in the query string (?tmdb_id= or ?id=), which the
    // router parses off before matching, so a plain path works here. The page
    // reads the params via route.result.querystring.params.
    { path: 'video/movie-details', component: MovieDetailsPage },
    // Shows carry only a tmdb_id: a show has no local catalog row yet, so the
    // details view is always live from TMDB (D-015).
    { path: 'video/show-details', component: ShowDetailsPage },
    // A season is addressed by local ?id= once its snapshot row exists, else
    // by ?tmdb_id= (the SHOW's) + ?season= together.
    { path: 'video/season-details', component: SeasonDetailsPage },
    // An episode is addressed by its season — ?season_id=, or the SHOW's
    // ?tmdb_id= + ?season= — plus ?episode=. It has no local id of its own:
    // there is no episodes table until M2.
    { path: 'video/episode-details', component: EpisodeDetailsPage },
    // A person carries only a ?tmdb_id=: there is no people table and never
    // will be — every view is live from TMDB (D-027).
    { path: 'video/person-details', component: PersonDetailsPage },
    { path: 'games', component: ComingSoon, props: { title: 'Games' } },
    { path: 'settings', component: SettingsPage },
    // The chooser before its one format page — same prefix-before-path rule.
    { path: 'settings/restore', component: RestorePage },
    { path: 'settings/restore/ticket-box', component: TicketBoxRestorePage },
    { path: 'settings/data-sources', component: DataSourcesPage },
    // The TMDB link callback lands here with ?tmdb_link=<code> (D-029).
    { path: 'settings/tmdb', component: TMDBAccountPage },
    // The Jellyfin library scan's status and "scan now" (D-033).
    { path: 'settings/jellyfin', component: JellyfinPage },
    { path: 'settings/users', component: UsersPage },
    { path: 'settings/account', component: AccountPage },
  ]

  const statuses = {
    [StatusCode.NotFound]: () => ({ component: NotFound }),
  }

  // The active class must be passed explicitly, or the action adds "undefined".
  // absolute: exact-path matching, so '/video' is not highlighted on '/video/history'.
  const activeExact = { active: { class: 'active', absolute: true } }

  // Prefix matching, so Settings and Lists stay highlighted on their sub-pages.
  const activePrefix = { active: { class: 'active' } }

  // Mobile drawer state. Below 768px the aside becomes an off-canvas drawer;
  // on desktop these have no effect (the topbar/backdrop are display:none and
  // the aside ignores .open).
  let menuOpen = $state(false)
  const closeMenu = () => (menuOpen = false)

  // Scroll lock lives on <body>, outside this component's DOM, hence the
  // effect. The class only does anything inside the mobile media query, so a
  // stale value after resizing to desktop is harmless.
  $effect(() => {
    document.body.classList.toggle('drawer-open', menuOpen)
    return () => document.body.classList.remove('drawer-open')
  })

  // Boot probe: one round-trip decides between setup, login and the shell.
  // The router below only mounts once authenticated, so the current URL
  // survives a login — deep links keep working with no redirect logic.
  async function probeSession() {
    session.status = 'unknown'
    try {
      applyStatus(await getSessionStatus())
    } catch {
      markSessionError()
    }
  }

  $effect(() => {
    probeSession()
  })
</script>

<svelte:window
  onkeydown={(e) => {
    if (menuOpen && e.key === 'Escape') closeMenu()
  }}
/>

{#if session.status === 'unknown'}
  <div class="boot">
    <div class="boot-brand">
      <img src="./icons/logo-mark.svg" alt="" />
      <span>sofalog</span>
    </div>
  </div>
{:else if session.status === 'error'}
  <div class="boot">
    <div class="boot-brand">
      <img src="./icons/logo-mark.svg" alt="" />
      <span>sofalog</span>
    </div>
    <p class="boot-error">Could not reach the server.</p>
    <button class="boot-retry" onclick={probeSession}>Retry</button>
  </div>
{:else if session.status === 'setup_required'}
  <SetupPage />
{:else if session.status === 'unauthenticated'}
  <LoginPage />
{:else}
  <div class="app">
    <header class="topbar">
      <button
        class="menu-button"
        aria-label="Open navigation"
        aria-expanded={menuOpen}
        aria-controls="sidebar"
        onclick={() => (menuOpen = !menuOpen)}
      >
        <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
          <path
            d="M4 6h16M4 12h16M4 18h16"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
          />
        </svg>
      </button>
      <a href="/" use:route class="brand" onclick={closeMenu}>
        <img src="./icons/logo-mark.svg" alt="" />
        <span>sofalog</span>
      </a>
    </header>

    <button
      class="backdrop"
      class:visible={menuOpen}
      tabindex="-1"
      aria-label="Close navigation"
      onclick={closeMenu}
    ></button>

    <aside id="sidebar" class:open={menuOpen}>
      <a href="/" use:route class="brand" onclick={closeMenu}>
        <img src="./icons/logo-mark.svg" alt="" />
        <span>sofalog</span>
      </a>

      <nav>
        <div class="group">
          <span class="label">Video</span>
          <a href="/video" use:route={activeExact} onclick={closeMenu}>Up Next To Watch</a>
          <a href="/video/upcoming" use:route={activeExact} onclick={closeMenu}>Upcoming</a>
          <a href="/video/history" use:route={activeExact} onclick={closeMenu}>History</a>
          <!-- Prefix matching: Lists stays highlighted on each list's page. -->
          <a href="/video/lists" use:route={activePrefix} onclick={closeMenu}>Lists</a>
          <a href="/video/trending" use:route={activeExact} onclick={closeMenu}>Trending</a>
          <a href="/video/stats" use:route={activeExact} onclick={closeMenu}>Stats</a>
          <a href="/video/search" use:route={activeExact} onclick={closeMenu}>Search</a>
        </div>

        <div class="group">
          <span class="label">Games</span>
          <a href="/games" use:route={activeExact} onclick={closeMenu}>Library</a>
        </div>
      </nav>

      <!-- Identity chip: the logged-in user, and the app's only link to
         Settings — which is where logging out lives. The row's margin-top:auto
         is what pins it and the version block to the bottom of the flex column.
         title=, because the link's text is a person's name, not a destination. -->
      <div class="user-row">
        <a
          href="/settings"
          use:route={activePrefix}
          class="user-chip"
          title="Settings"
          onclick={closeMenu}
        >
          <span class="avatar" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="16" height="16">
              <circle cx="12" cy="8.5" r="3.5" fill="currentColor" />
              <path d="M4.5 20.2a7.5 7.5 0 0 1 15 0" fill="currentColor" />
            </svg>
          </span>
          <span class="user-name">{displayLabel(session.user)}</span>
        </a>
      </div>

      <div class="version">
        <span class="version-label">App version</span>
        <span class="version-number">v0.0.1</span>
      </div>
    </aside>

    <main>
      <Router {routes} {statuses} />
    </main>
  </div>
{/if}

<style>
  .app {
    min-height: 100dvh;
    display: flex;
  }

  /* Boot splash / boot error — shown before the session probe resolves. */
  .boot {
    min-height: 100dvh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1rem;
  }

  .boot-brand {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 1.3rem;
    font-weight: 600;
    animation: skeleton-pulse 1.5s ease-in-out infinite;
  }

  .boot-brand img {
    width: 2rem;
    height: 2rem;
    border-radius: var(--radius-sm);
  }

  .boot-error {
    margin: 0;
    color: var(--err);
    font-size: 0.9rem;
  }

  /* The error state holds the brand still. */
  .boot:has(.boot-error) .boot-brand {
    animation: none;
  }

  .boot-retry {
    padding: 0.45rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 0.9rem;
    cursor: pointer;
  }

  .boot-retry:hover {
    border-color: var(--accent);
  }

  aside {
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    width: 13rem;
    flex-shrink: 0;
    padding: 1rem;
    border-right: 1px solid var(--border);
    /* One viewport tall and pinned, so the bottom-anchored user/version block
       stays reachable however long the page beside it gets. Without the
       explicit height the aside stretches to the whole DOCUMENT (.app is a flex
       row with min-height, not height), which parks .user-row's margin-top:auto
       at the end of the scroll rather than the end of the screen. The height is
       also what stops the flex stretch, which sticky needs to be able to move.
       Own scroll when the window is too short for the nav; contain stops that
       scroll chaining to the page behind it. */
    position: sticky;
    top: 0;
    height: 100dvh;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 1.15rem;
    font-weight: 600;
    color: var(--text);
    text-decoration: none;
  }

  .brand img {
    width: 1.75rem;
    height: 1.75rem;
    border-radius: var(--radius-sm);
  }

  nav {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .label {
    margin-bottom: 0.25rem;
    padding-left: 0.75rem; /* aligns with the link text next to the rail gutter */
    color: var(--text-dim);
    opacity: 0.85;
    font-size: 0.65rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  nav a {
    position: relative; /* anchor for the active item's accent rail */
    padding: 0.3rem 0.5rem 0.3rem 0.75rem;
    border-radius: var(--radius-sm);
    color: var(--text-dim);
    text-decoration: none;
    font-size: 0.9rem;
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }

  nav a:hover {
    color: var(--text);
    background: var(--surface);
  }

  /* :global — the router's route action adds the class at runtime. The accent
     tint keeps the selected item distinct from the plain --surface hover. */
  nav :global(a.active) {
    color: var(--accent-soft);
    background: var(--accent-tint);
    font-weight: 600;
  }

  nav :global(a.active):hover {
    background: var(--accent-tint-strong);
  }

  nav :global(a.active)::before {
    content: '';
    position: absolute;
    left: 0;
    top: 20%;
    bottom: 20%;
    width: 3px;
    border-radius: 2px;
    background: var(--accent);
  }

  /* auto top margin: pushes this and the version block to the bottom of the
     sidebar. */
  .user-row {
    margin-top: auto;
    display: flex;
    align-items: center;
  }

  .user-chip {
    flex: 1;
    min-width: 0; /* lets the name ellipsis instead of widening the row */
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.45rem 0.5rem;
    border-radius: var(--radius-sm);
    color: var(--text-dim);
    text-decoration: none;
  }

  .user-chip:hover {
    background: var(--surface);
    color: var(--text);
  }

  /* :global — the router's route action adds the class at runtime. Same tokens
     as the nav links above, but no ::before rail (the chip's padding has no
     rail gutter) and no font-weight bump, which would reflow the name's
     ellipsis. */
  .user-row :global(a.active) {
    color: var(--accent-soft);
    background: var(--accent-tint);
  }

  .user-row :global(a.active):hover {
    background: var(--accent-tint-strong);
  }

  .avatar {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.9rem;
    height: 1.9rem;
    flex-shrink: 0;
    border-radius: 50%;
    overflow: hidden; /* clips the bust's shoulders to the circle */
    background: linear-gradient(135deg, #38bdf8, var(--accent));
    color: rgb(255 255 255 / 0.9);
  }

  .avatar svg {
    margin-top: 3px; /* seats the bust at the bottom of the circle */
  }

  .user-name {
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .version {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    padding: 0.75rem 0.5rem 0;
    border-top: 1px solid var(--border);
  }

  .version-label {
    color: var(--text-dim);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .version-number {
    color: var(--text-dim);
    font-size: 0.75rem;
    font-variant-numeric: tabular-nums;
  }

  main {
    flex: 1;
    min-width: 0;
    padding: var(--space-page);
    /* Safe areas matter once viewport-fit=cover is set: landscape notch on the
       sides, home indicator at the bottom. max()/calc() fall back cleanly where
       env() is 0. */
    padding-left: max(var(--space-page), env(safe-area-inset-left));
    padding-right: max(var(--space-page), env(safe-area-inset-right));
    padding-bottom: calc(var(--space-page) + env(safe-area-inset-bottom));
  }

  /* Desktop default: no topbar, no backdrop, the aside is a plain sidebar. */
  .topbar,
  .backdrop {
    display: none;
  }

  /* Mobile/tablet: sticky topbar with a hamburger; the aside becomes an
     off-canvas drawer. z-index ladder: topbar 10 < backdrop 20 < drawer 30. */
  @media (max-width: 767px) {
    .app {
      flex-direction: column;
    }

    .topbar {
      display: flex;
      align-items: center;
      gap: 0.25rem;
      position: sticky;
      top: 0;
      z-index: 10;
      padding: 0.4rem 0.5rem;
      padding-top: calc(0.4rem + env(safe-area-inset-top));
      background: var(--bg);
      border-bottom: 1px solid var(--border);
    }

    .menu-button {
      display: flex;
      align-items: center;
      justify-content: center;
      min-width: 2.75rem;
      min-height: 2.75rem;
      padding: 0;
      background: none;
      border: none;
      border-radius: var(--radius-sm);
      color: var(--text);
      cursor: pointer;
    }

    .menu-button:hover {
      background: var(--surface);
    }

    aside {
      position: fixed;
      /* Full height without 100vh (iOS URL-bar problem); agrees with the base
         rule's 100dvh, whose overflow-y and overscroll-behavior carry over. */
      inset: 0 auto 0 0;
      z-index: 30;
      width: min(16rem, 85vw);
      background: var(--bg);
      border-right: 1px solid var(--border);
      padding-top: calc(1rem + env(safe-area-inset-top));
      padding-bottom: calc(1rem + env(safe-area-inset-bottom));
      transform: translateX(-100%);
      /* visibility joins the transition so the close animation still plays,
         while a closed drawer's links leave the tab order — no inert needed. */
      visibility: hidden;
      transition:
        transform 0.25s ease,
        visibility 0.25s;
    }

    aside.open {
      transform: none;
      visibility: visible;
    }

    .backdrop {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 20;
      padding: 0;
      border: none;
      background: rgb(0 0 0 / 0.5);
      opacity: 0;
      visibility: hidden;
      transition:
        opacity 0.25s,
        visibility 0.25s;
    }

    .backdrop.visible {
      opacity: 1;
      visibility: visible;
    }

    /* Scroll lock while the drawer is open. Scoped to this media query so a
       stale class after resizing to desktop does nothing. */
    :global(body.drawer-open) {
      overflow: hidden;
    }
  }
</style>
