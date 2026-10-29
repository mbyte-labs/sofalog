<script>
  import { ApiError } from '../../lib/api/client.js'
  import {
    displayLabel,
    readableMessage,
    setDisplayName,
    setEmail,
    setMatchWatchlist,
    setPassword,
    setTimezone,
  } from './api.js'
  import { applyStatus, session } from './session.svelte.js'
  import { getSessionStatus } from './api.js'

  // Seeded from the session and re-seeded whenever it changes server-side.
  let nameValue = $state(session.user?.display_name ?? '')
  let nameBusy = $state(false)
  let nameError = $state('')
  let nameDone = $state(false)

  let emailValue = $state(session.user?.email ?? '')
  let emailPassword = $state('')
  let emailBusy = $state(false)
  let emailError = $state('')
  let emailDone = $state(false)

  /** @param {unknown} err @param {string} fallback */
  function messageFor(err, fallback) {
    if (!(err instanceof ApiError)) return 'Could not reach the server.'
    if (err.code === 'conflict') return 'That email is already registered.'
    if (err.code === 'validation') return readableMessage(err.message)
    return `${fallback} (${err.code}).`
  }

  // The probe is the source of truth for the session user, so re-run it after
  // changing identity rather than patching the store by hand.
  async function refreshSession() {
    try {
      applyStatus(await getSessionStatus())
    } catch {
      /* the next navigation will re-probe */
    }
  }

  /** @param {SubmitEvent} e */
  async function submitName(e) {
    e.preventDefault()
    const id = session.user?.id
    if (id === undefined) return
    nameBusy = true
    nameError = ''
    nameDone = false
    try {
      await setDisplayName(id, nameValue)
      await refreshSession()
      nameDone = true
    } catch (err) {
      nameError = messageFor(err, 'Could not set the display name')
    } finally {
      nameBusy = false
    }
  }

  /** @param {SubmitEvent} e */
  async function submitEmail(e) {
    e.preventDefault()
    const id = session.user?.id
    if (id === undefined) return
    emailBusy = true
    emailError = ''
    emailDone = false
    try {
      await setEmail(id, emailValue, emailPassword)
      emailPassword = ''
      await refreshSession()
      emailDone = true
    } catch (err) {
      emailError = messageFor(err, 'Could not change the email')
    } finally {
      emailBusy = false
    }
  }

  // The matching switch saves on change — there is nothing to type, so a
  // Save button would be a second tap for the same intent. The checkbox is
  // NOT bound: the session is the source of truth, refreshed after the save,
  // and a failure puts the box back where the server still has it.
  let matchBusy = $state(false)
  let matchError = $state('')

  /** @param {Event} e */
  async function toggleMatch(e) {
    const input = /** @type {HTMLInputElement} */ (e.currentTarget)
    const id = session.user?.id
    if (id === undefined) return
    const on = input.checked
    matchBusy = true
    matchError = ''
    try {
      await setMatchWatchlist(id, on)
      await refreshSession()
    } catch (err) {
      input.checked = !on
      matchError = messageFor(err, 'Could not change the setting')
    } finally {
      matchBusy = false
    }
  }

  // The time zone (D-038) saves on change, like the matching switch and for
  // the same reasons: the select is NOT bound, the refreshed session is what
  // it shows, and a failure puts it back. '' is "server default" (null).
  let zoneBusy = $state(false)
  let zoneError = $state('')

  // The browser's list can lack UTC (Chrome omits it) and can lack a stored
  // name the server accepted under an older alias, so both are added.
  const zoneOptions = $derived.by(() => {
    const zones = new Set(Intl.supportedValuesOf('timeZone'))
    zones.add('UTC')
    if (session.user?.timezone) zones.add(session.user.timezone)
    return [...zones].sort()
  })
  const deviceZone = Intl.DateTimeFormat().resolvedOptions().timeZone

  /** @param {string | null} zone */
  async function saveZone(zone) {
    const id = session.user?.id
    if (id === undefined) return
    zoneBusy = true
    zoneError = ''
    try {
      await setTimezone(id, zone)
      await refreshSession()
      return true
    } catch (err) {
      zoneError = messageFor(err, 'Could not change the time zone')
      return false
    } finally {
      zoneBusy = false
    }
  }

  /** @param {Event} e */
  async function changeZone(e) {
    const select = /** @type {HTMLSelectElement} */ (e.currentTarget)
    const ok = await saveZone(select.value || null)
    if (!ok) select.value = session.user?.timezone ?? ''
  }

  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let busy = $state(false)
  let error = $state('')
  let done = $state(false)

  /** @param {SubmitEvent} e */
  async function submit(e) {
    e.preventDefault()
    if (newPassword !== confirmPassword) {
      error = 'The new passwords do not match.'
      return
    }
    const id = session.user?.id
    if (id === undefined) return
    busy = true
    error = ''
    done = false
    try {
      await setPassword(id, { currentPassword, newPassword })
      done = true
      currentPassword = ''
      newPassword = ''
      confirmPassword = ''
    } catch (err) {
      // A wrong current password is 422 `validation`, never 401 — this form
      // reports it inline instead of the app dropping to the login screen.
      error =
        err instanceof ApiError
          ? err.code === 'validation'
            ? readableMessage(err.message)
            : `Could not change the password (${err.code}).`
          : 'Could not reach the server.'
    } finally {
      busy = false
    }
  }
</script>

<section>
  <div class="page-head">
    <h1>Account</h1>
  </div>

  {#if session.auth.oidc}
    <p class="blurb">
      Signed in as <strong>{displayLabel(session.user)}</strong> ({session.user?.email}) through
      {session.auth.oidc_name ?? 'single sign-on'}. Your password and email address are managed
      there, not here — this server stores no password for your account.
    </p>
  {:else}
    <p class="blurb">
      Signed in as <strong>{displayLabel(session.user)}</strong> ({session.user?.email}). Changing
      your password signs you out on your other devices; this one stays signed in.
    </p>

    <form onsubmit={submit}>
      <label>
        <span>Current password</span>
        <input
          type="password"
          bind:value={currentPassword}
          autocomplete="current-password"
          required
        />
      </label>
      <label>
        <span>New password</span>
        <input type="password" bind:value={newPassword} autocomplete="new-password" required />
      </label>
      <label>
        <span>Confirm new password</span>
        <input type="password" bind:value={confirmPassword} autocomplete="new-password" required />
      </label>
      {#if error}
        <p class="status error">{error}</p>
      {/if}
      {#if done}
        <p class="status ok">Password changed. Your other devices have been signed out.</p>
      {/if}
      <button type="submit" disabled={busy}>{busy ? 'Saving…' : 'Change password'}</button>
    </form>

    <h2>Email</h2>
    <p class="blurb">This is what you sign in with, so it needs your current password.</p>
    <form onsubmit={submitEmail}>
      <label>
        <span>Email</span>
        <input type="email" bind:value={emailValue} autocomplete="username" required />
      </label>
      <label>
        <span>Current password</span>
        <input
          type="password"
          bind:value={emailPassword}
          autocomplete="current-password"
          required
        />
      </label>
      {#if emailError}
        <p class="status error">{emailError}</p>
      {/if}
      {#if emailDone}
        <p class="status ok">Email updated. Use it next time you sign in.</p>
      {/if}
      <button type="submit" disabled={emailBusy}>{emailBusy ? 'Saving…' : 'Change email'}</button>
    </form>
  {/if}

  <h2>Display name</h2>
  <p class="blurb">
    How you appear in the app. Just a label — it need not be unique, and leaving it empty falls back
    to your email.
  </p>
  <form onsubmit={submitName}>
    <label>
      <span>Display name</span>
      <input type="text" bind:value={nameValue} autocomplete="name" />
    </label>
    {#if nameError}
      <p class="status error">{nameError}</p>
    {/if}
    {#if nameDone}
      <p class="status ok">Display name updated.</p>
    {/if}
    <button type="submit" disabled={nameBusy}>{nameBusy ? 'Saving…' : 'Save display name'}</button>
  </form>

  <h2>Matching watchlist</h2>
  <p class="blurb">
    Lets other people on this server pick you in their watchlist's "Matching watchlist" filter and
    see which of their watchlisted titles you have too. They never see your list — only what the two
    of you have in common. Off by default, and only you can change it.
  </p>
  <div class="card">
    <label class="check">
      <input
        type="checkbox"
        checked={session.user?.match_watchlist ?? false}
        disabled={matchBusy}
        onchange={toggleMatch}
      />
      <span>Allow matching watchlist</span>
    </label>
    {#if matchError}
      <p class="status error">{matchError}</p>
    {/if}
  </div>

  <h2>Time zone</h2>
  <p class="blurb">
    Where your day ends: which episodes count as aired, what the calendar calls "Today", and the
    zone every date and time is shown in. Everything is stored in UTC, so changing this moves
    nothing you have logged — only how it reads.
  </p>
  <div class="card">
    <label>
      <span>Time zone</span>
      <select value={session.user?.timezone ?? ''} disabled={zoneBusy} onchange={changeZone}>
        <option value=""
          >Server default{session.user?.timezone
            ? ''
            : ` (${session.user?.effective_timezone})`}</option
        >
        {#each zoneOptions as zone (zone)}
          <option value={zone}>{zone.replaceAll('_', ' ')}</option>
        {/each}
      </select>
    </label>
    {#if deviceZone && deviceZone !== session.user?.effective_timezone}
      <p class="hint">
        This device is set to {deviceZone.replaceAll('_', ' ')}.
        <button
          type="button"
          class="link"
          disabled={zoneBusy || !zoneOptions.includes(deviceZone)}
          onclick={() => saveZone(deviceZone)}>Use it</button
        >
      </p>
    {/if}
    {#if zoneError}
      <p class="status error">{zoneError}</p>
    {/if}
  </div>
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  h2 {
    margin: 1.75rem 0 0.5rem;
    font-size: 1rem;
    font-weight: 600;
  }

  .blurb {
    margin: 0 0 1rem;
    max-width: 40rem;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.5;
  }

  form,
  .card {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
    max-width: 22rem;
    padding: 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
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

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  select {
    padding: 0.5rem 0.6rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: 0.95rem;
  }

  .hint {
    margin: 0;
    font-size: 0.85rem;
    color: var(--text-dim);
  }

  button.link {
    padding: 0;
    background: none;
    color: var(--accent);
    font-size: inherit;
    font-weight: 600;
    text-decoration: underline;
  }

  button.link:hover:not(:disabled) {
    background: none;
  }

  /* A switch reads left-to-right, unlike the stacked text fields above. */
  .check {
    flex-direction: row;
    align-items: center;
    gap: 0.6rem;
    color: var(--text);
    font-size: 0.95rem;
    cursor: pointer;
  }

  .check input {
    margin: 0;
    padding: 0;
    width: 1.15rem;
    height: 1.15rem;
    border: none;
    background: none;
    accent-color: var(--accent);
    cursor: pointer;
  }

  .check input:disabled {
    cursor: default;
  }

  @media (hover: none) {
    .check {
      min-height: 2.75rem;
    }
  }

  .status {
    margin: 0;
    font-size: 0.85rem;
  }

  .status.error {
    color: var(--err);
  }

  .status.ok {
    color: var(--ok);
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
