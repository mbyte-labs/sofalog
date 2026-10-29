<script>
  import { ApiError } from '../../lib/api/client.js'
  import { getSessionStatus, readableMessage, setup } from './api.js'
  import { applyStatus, setAuthenticated } from './session.svelte.js'
  import AuthShell from './components/AuthShell.svelte'

  let email = $state('')
  let displayName = $state('')
  let password = $state('')
  let confirm = $state('')
  let busy = $state(false)
  let error = $state('')

  /** @param {SubmitEvent} e */
  async function submit(e) {
    e.preventDefault()
    if (password !== confirm) {
      error = 'The passwords do not match.'
      return
    }
    busy = true
    error = ''
    try {
      const res = await setup(email, password, displayName)
      setAuthenticated(res.user)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'conflict') {
        // Someone else finished setup first (another tab, another device).
        // Re-probe: the app flips to the login page.
        applyStatus(await getSessionStatus().catch(() => ({ status: 'unauthenticated' })))
        return
      }
      error =
        err instanceof ApiError
          ? readableMessage(err.message) // validation messages (address shape, password length) are actionable as-is
          : 'Could not reach the server.'
    } finally {
      busy = false
    }
  }
</script>

<AuthShell title="Create the first account">
  <p class="blurb">
    This instance has no users yet. The account you create here is the admin — it can add accounts
    for the rest of the household later, under Settings.
  </p>
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
      <span>Display name <span class="optional">(optional)</span></span>
      <input type="text" bind:value={displayName} autocomplete="name" />
    </label>
    <label>
      <span>Password</span>
      <input type="password" bind:value={password} autocomplete="new-password" required />
    </label>
    <label>
      <span>Confirm password</span>
      <input type="password" bind:value={confirm} autocomplete="new-password" required />
    </label>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <button type="submit" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</button>
  </form>
</AuthShell>

<style>
  .optional {
    opacity: 0.7;
  }

  .blurb {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.85rem;
    line-height: 1.4;
  }

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
