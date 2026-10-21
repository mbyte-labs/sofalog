<script>
  import { route } from '@mateothegreat/svelte5-router'
  import { logout } from '../auth/api.js'
  import { clearSession, session } from '../auth/session.svelte.js'

  // One entry per settings page. Adding a section is a line here plus a route
  // in App.svelte — the sidebar reaches Settings through the account chip
  // either way, so it never grows an entry per section.
  const sections = $derived([
    {
      href: '/settings/data-sources',
      title: 'Data Sources',
      blurb:
        'The third-party services sofalog fetches metadata and artwork from, and what each one provides.',
    },
    {
      href: '/settings/restore',
      title: 'Restore backup',
      blurb: 'Bring your watch history, ratings and watchlist back from a Ticket Box export.',
    },
    {
      href: '/settings/tmdb',
      title: 'TMDB account',
      blurb:
        'Link your own TMDB account: import what it holds once, and keep your ratings, watchlist and favorites in sync.',
    },
    {
      href: '/settings/jellyfin',
      title: 'Jellyfin',
      blurb: 'Mark what is on your Jellyfin server, and scan it now.',
    },
    {
      href: '/settings/account',
      title: 'Account',
      // Under single sign-on there is no password here to change.
      blurb: session.auth.oidc
        ? 'Who you are signed in as, and where that comes from.'
        : 'Change your password.',
    },
    // Registration is closed; the admin is the only one who can add accounts.
    ...(session.user?.is_admin
      ? [
          {
            href: '/settings/users',
            title: 'Users',
            blurb: session.auth.oidc
              ? 'See accounts and set the emails single sign-on matches on.'
              : 'Add accounts, reset passwords, remove people.',
          },
        ]
      : []),
  ])

  async function doLogout() {
    try {
      await logout()
    } finally {
      // Even if the server was unreachable, drop the local session — the
      // next boot probe re-syncs with reality.
      clearSession()
    }
  }
</script>

<section>
  <div class="page-head">
    <h1>Settings</h1>
  </div>

  <ul class="sections">
    {#each sections as section (section.href)}
      <li>
        <a href={section.href} use:route>
          <span class="title">{section.title}</span>
          <span class="blurb">{section.blurb}</span>
        </a>
      </li>
    {/each}
  </ul>

  <!-- Sign-out lives here rather than in the `sections` list above: every
     entry there is a route link, and this is the page's one action. -->
  <button type="button" class="logout" onclick={doLogout}>
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <path
        d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
    Log out
  </button>
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  .sections {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
    max-width: 40rem;
  }

  .sections a {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    padding: 0.75rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-decoration: none;
    transition:
      border-color var(--ease-lift),
      transform var(--ease-lift);
  }

  .sections a:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }

  .title {
    color: var(--text);
    font-weight: 600;
  }

  .blurb {
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

  /* Same card treatment as the section links, but fit-content wide so it does
     not read as a fourth destination. */
  .logout {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: fit-content; /* a block-level flex button would otherwise stretch */
    margin-top: 1.5rem;
    padding: 0.75rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    transition:
      border-color var(--ease-lift),
      transform var(--ease-lift);
  }

  .logout:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }
</style>
