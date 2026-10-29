<script>
  import { ApiError } from '../../lib/api/client.js'
  import { login, ssoLoginUrl } from './api.js'
  import { session, setAuthenticated } from './session.svelte.js'
  import AuthShell from './components/AuthShell.svelte'

  // The callback redirects here with ?sso_error= when sign-in fails; it
  // cannot render JSON at a browser mid-redirect, so the code travels in the
  // URL and is turned into a sentence here.
  const ssoError = new URLSearchParams(window.location.search).get('sso_error')

  /** @param {string} code */
  function ssoErrorMessage(code) {
    switch (code) {
      case 'expired':
        return 'That sign-in attempt expired. Please try again.'
      case 'provider_unavailable':
        return 'Could not reach the sign-in provider. Try again shortly.'
      case 'access_denied':
      case 'rejected':
        return 'The provider refused the sign-in.'
      default:
        return 'Single sign-on failed. Please try again.'
    }
  }

  let email = $state('')
  let password = $state('')
  let busy = $state(false)
  let error = $state('')

  /** @param {unknown} err */
  function loginErrorMessage(err) {
    if (!(err instanceof ApiError)) return 'Could not reach the server.'
    // Deliberately the same for an unknown address and a wrong password —
    // the backend does not reveal which are registered, and neither do we.
    if (err.code === 'unauthorized') return 'Wrong email or password.'
    return `Could not log in (${err.code}).`
  }

  /** @param {SubmitEvent} e */
  async function submit(e) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      const res = await login(email, password)
      // Flips App.svelte to the shell; the current URL survives, so a deep
      // link opened while logged out lands where it pointed.
      setAuthenticated(res.user)
    } catch (err) {
      error = loginErrorMessage(err)
    } finally {
      busy = false
    }
  }
</script>

<AuthShell title="Log in">
  {#if ssoError}
    <p class="error">{ssoErrorMessage(ssoError)}</p>
  {/if}

  {#if session.auth.oidc}
    <p class="sso-blurb">
      This server signs in through {session.auth.oidc_name ?? 'single sign-on'}.
    </p>
    <a class="sso" href={ssoLoginUrl(window.location.pathname)}>
      Continue with {session.auth.oidc_name ?? 'single sign-on'}
    </a>
  {/if}

  {#if session.auth.local}
    <form onsubmit={submit}>
      <label>
        <span>Email</span>
        <!-- svelte-ignore a11y_autofocus — the form is the whole page -->
        <input
          type="email"
          bind:value={email}
          autocomplete="username"
          autocapitalize="off"
          required
          autofocus
        />
      </label>
      <label>
        <span>Password</span>
        <input type="password" bind:value={password} autocomplete="current-password" required />
      </label>
      {#if error}
        <p class="error">{error}</p>
      {/if}
      <button type="submit" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
    </form>
  {/if}
</AuthShell>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  input {
    padding: 0.5rem 0.6rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: 0.95rem;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .error {
    margin: 0;
    color: var(--err);
    font-size: 0.85rem;
  }

  .sso-blurb {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

  /* An anchor, not a button: this is a full browser navigation into the
     provider's redirect chain, which fetch cannot follow. */
  .sso {
    display: block;
    padding: 0.55rem;
    background: var(--cta);
    border-radius: var(--radius-md);
    color: #fff;
    font-size: 0.95rem;
    font-weight: 600;
    text-align: center;
    text-decoration: none;
    transition: background var(--ease-lift);
  }

  .sso:hover {
    background: var(--cta-strong);
  }

  button {
    padding: 0.55rem;
    background: var(--cta);
    border: none;
    border-radius: var(--radius-md);
    color: #fff;
    font-size: 0.95rem;
    font-weight: 600;
    cursor: pointer;
    transition: background var(--ease-lift);
  }

  button:hover:not(:disabled) {
    background: var(--cta-strong);
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
