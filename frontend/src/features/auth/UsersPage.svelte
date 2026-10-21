<script>
  import { ApiError } from '../../lib/api/client.js'
  import { formatInstant } from '../../lib/time.js'
  import {
    createUser,
    deleteUser,
    displayLabel,
    listUsers,
    readableMessage,
    setDisplayName,
    setEmail,
    setPassword,
  } from './api.js'
  import { session } from './session.svelte.js'

  /**
   * @type {{ state: 'loading' }
   *   | { state: 'loaded', users: import('./api.js').UserListEntry[] }
   *   | { state: 'error', detail: string }}
   */
  let view = $state({ state: 'loading' })

  // Guards out-of-order responses: a mutation reloads the list while an
  // earlier load may still be in flight.
  let seq = 0

  // Which row has its password panel open, and which is awaiting delete
  // confirmation. Only one of each at a time.
  let passwordFor = $state(/** @type {number | null} */ (null))
  let emailFor = $state(/** @type {number | null} */ (null))
  let nameFor = $state(/** @type {number | null} */ (null))
  let nameValue = $state('')
  let confirmingDelete = $state(/** @type {number | null} */ (null))
  let emailValue = $state('')

  let newPassword = $state('')
  let confirmPassword = $state('')
  let rowBusy = $state(false)
  let rowError = $state('')
  let notice = $state('')

  // Create form.
  let email = $state('')
  let password = $state('')
  let displayName = $state('')
  let creating = $state(false)
  let createError = $state('')

  function load() {
    const id = ++seq
    listUsers()
      .then(({ users }) => {
        if (id === seq) view = { state: 'loaded', users }
      })
      .catch((err) => {
        if (id === seq)
          view = { state: 'error', detail: err instanceof ApiError ? err.code : 'unreachable' }
      })
  }

  $effect(() => {
    load()
  })

  /** @param {unknown} err @param {string} fallback */
  function messageFor(err, fallback) {
    if (!(err instanceof ApiError)) return 'Could not reach the server.'
    if (err.code === 'conflict') return 'That email is already registered.'
    // Validation messages (address shape, password length) are actionable as-is.
    if (err.code === 'validation') return readableMessage(err.message)
    return `${fallback} (${err.code}).`
  }

  /** @param {string} iso */
  function joined(iso) {
    return formatInstant(iso, { year: 'numeric', month: 'short', day: 'numeric' })
  }

  function closeRow() {
    passwordFor = null
    emailFor = null
    nameFor = null
    confirmingDelete = null
    newPassword = ''
    confirmPassword = ''
    rowError = ''
  }

  /** @param {number} id */
  function openPassword(id) {
    closeRow()
    notice = ''
    passwordFor = id
  }

  /** @param {import('./api.js').UserListEntry} user */
  function openName(user) {
    closeRow()
    notice = ''
    nameValue = user.display_name
    nameFor = user.id
  }

  /** @param {SubmitEvent} e @param {import('./api.js').UserListEntry} user */
  async function submitName(e, user) {
    e.preventDefault()
    rowBusy = true
    rowError = ''
    try {
      await setDisplayName(user.id, nameValue)
      notice = nameValue
        ? `Renamed to “${nameValue}”.`
        : `Cleared the display name for ${user.email}.`
      closeRow()
      load()
    } catch (err) {
      rowError = messageFor(err, 'Could not set the display name')
    } finally {
      rowBusy = false
    }
  }

  /** @param {import('./api.js').UserListEntry} user */
  function openEmail(user) {
    closeRow()
    notice = ''
    emailValue = user.email
    emailFor = user.id
  }

  /** @param {SubmitEvent} e @param {import('./api.js').UserListEntry} user */
  async function submitEmail(e, user) {
    e.preventDefault()
    rowBusy = true
    rowError = ''
    try {
      await setEmail(user.id, emailValue)
      notice = `${displayLabel(user)} now signs in as ${emailValue}.`
      closeRow()
      load()
    } catch (err) {
      rowError = messageFor(err, 'Could not set the email')
    } finally {
      rowBusy = false
    }
  }

  /** @param {SubmitEvent} e @param {import('./api.js').UserListEntry} user */
  async function submitPassword(e, user) {
    e.preventDefault()
    if (newPassword !== confirmPassword) {
      rowError = 'The passwords do not match.'
      return
    }
    rowBusy = true
    rowError = ''
    try {
      await setPassword(user.id, { newPassword })
      notice = `Set a new password for ${displayLabel(user)}. They have been signed out everywhere.`
      closeRow()
    } catch (err) {
      rowError = messageFor(err, 'Could not set the password')
    } finally {
      rowBusy = false
    }
  }

  /** @param {import('./api.js').UserListEntry} user */
  async function confirmDelete(user) {
    rowBusy = true
    rowError = ''
    try {
      await deleteUser(user.id)
      notice = `Deleted ${displayLabel(user)}.`
      closeRow()
      load()
    } catch (err) {
      rowError = messageFor(err, 'Could not delete the account')
    } finally {
      rowBusy = false
    }
  }

  /** @param {SubmitEvent} e */
  async function submitCreate(e) {
    e.preventDefault()
    creating = true
    createError = ''
    notice = ''
    try {
      const res = await createUser(email, password, displayName)
      notice = `Created ${displayLabel(res.user)}.`
      email = ''
      password = ''
      displayName = ''
      load()
    } catch (err) {
      createError = messageFor(err, 'Could not create the user')
    } finally {
      creating = false
    }
  }
</script>

<section>
  <div class="page-head">
    <h1>Users</h1>
  </div>

  {#if session.user?.is_admin}
    {#if session.auth.oidc}
      <p class="blurb">
        Accounts are provisioned by {session.auth.oidc_name ?? 'your identity provider'} on first sign-in,
        so there is nothing to create here. An account links to an identity by
        <strong>verified email</strong> — set the address below on an account that predates single sign-on
        and it will attach on that person's next sign-in instead of creating a duplicate.
      </p>
    {:else}
      <p class="blurb">
        Everyone with an account gets their own watch history, ratings and watchlist — nothing
        personal is shared between them. You can add accounts, reset a forgotten password, or remove
        someone entirely. To change your own password, use
        <strong>Settings → Account</strong>.
      </p>
    {/if}

    {#if notice}
      <p class="status ok">{notice}</p>
    {/if}

    {#if view.state === 'loading'}
      <ul class="users">
        {#each [1, 2] as n (n)}
          <li class="user skeleton"></li>
        {/each}
      </ul>
    {:else if view.state === 'error'}
      <p class="status error">Could not load the user list ({view.detail})</p>
    {:else}
      <ul class="users">
        {#each view.users as user (user.id)}
          {@const isSelf = user.id === session.user?.id}
          <li class="user">
            <div class="row">
              <span class="name">{displayLabel(user)}</span>
              {#if user.is_admin}<span class="badge admin">Admin</span>{/if}
              {#if isSelf}<span class="badge you">You</span>{/if}
              {#if user.linked}<span class="badge linked">SSO</span>{/if}
              <span class="joined">
                {user.email || 'no email'}{user.created_at
                  ? ` · joined ${joined(user.created_at)}`
                  : ''}
              </span>

              <div class="actions">
                {#if isSelf}
                  <!-- Your own password lives on the Account page (it needs the
                       current one), and the server refuses self-deletion. The
                       email is still editable here: it is what links this
                       account to an identity. -->
                  <button class="ghost" onclick={() => openName(user)}>Rename</button>
                  <span class="self-hint">Email &amp; password: Settings → Account</span>
                {:else if confirmingDelete === user.id}
                  <span class="confirm-text">Delete {displayLabel(user)}?</span>
                  <button class="danger" disabled={rowBusy} onclick={() => confirmDelete(user)}>
                    {rowBusy ? 'Deleting…' : 'Confirm'}
                  </button>
                  <button class="ghost" disabled={rowBusy} onclick={closeRow}>Cancel</button>
                {:else}
                  <button class="ghost" onclick={() => openName(user)}>Rename</button>
                  <button class="ghost" onclick={() => openEmail(user)}>Set email</button>
                  {#if session.auth.local}
                    <button class="ghost" onclick={() => openPassword(user.id)}>Set password</button
                    >
                  {/if}
                  <button
                    class="ghost danger-text"
                    onclick={() => {
                      closeRow()
                      notice = ''
                      confirmingDelete = user.id
                    }}>Delete</button
                  >
                {/if}
              </div>
            </div>

            {#if confirmingDelete === user.id}
              <p class="warn">
                Their watch history, ratings and watchlist are permanently deleted. Films only they
                tracked stay in the shared library.
              </p>
            {/if}

            {#if nameFor === user.id}
              <form class="panel" onsubmit={(e) => submitName(e, user)}>
                <label>
                  <span>Display name</span>
                  <!-- svelte-ignore a11y_autofocus — opened by an explicit click -->
                  <input type="text" bind:value={nameValue} autocomplete="off" autofocus />
                </label>
                <p class="hint">
                  Just a label — it need not be unique. Leave empty to fall back to the email.
                </p>
                <div class="panel-actions">
                  <button type="submit" disabled={rowBusy}>{rowBusy ? 'Saving…' : 'Save'}</button>
                  <button type="button" class="ghost" disabled={rowBusy} onclick={closeRow}>
                    Cancel
                  </button>
                </div>
              </form>
            {/if}

            {#if emailFor === user.id}
              <form class="panel" onsubmit={(e) => submitEmail(e, user)}>
                <label>
                  <span>Email</span>
                  <!-- svelte-ignore a11y_autofocus — opened by an explicit click -->
                  <input type="email" bind:value={emailValue} autocomplete="off" autofocus />
                </label>
                <p class="hint">This is how they sign in, and what an SSO login matches against.</p>
                <div class="panel-actions">
                  <button type="submit" disabled={rowBusy}>{rowBusy ? 'Saving…' : 'Save'}</button>
                  <button type="button" class="ghost" disabled={rowBusy} onclick={closeRow}>
                    Cancel
                  </button>
                </div>
              </form>
            {/if}

            {#if passwordFor === user.id}
              <form class="panel" onsubmit={(e) => submitPassword(e, user)}>
                <label>
                  <span>New password</span>
                  <!-- svelte-ignore a11y_autofocus — opened by an explicit click -->
                  <input
                    type="password"
                    bind:value={newPassword}
                    autocomplete="new-password"
                    required
                    autofocus
                  />
                </label>
                <label>
                  <span>Confirm password</span>
                  <input
                    type="password"
                    bind:value={confirmPassword}
                    autocomplete="new-password"
                    required
                  />
                </label>
                <p class="hint">{displayLabel(user)} will be signed out on every device.</p>
                <div class="panel-actions">
                  <button type="submit" disabled={rowBusy}>{rowBusy ? 'Saving…' : 'Save'}</button>
                  <button type="button" class="ghost" disabled={rowBusy} onclick={closeRow}>
                    Cancel
                  </button>
                </div>
              </form>
            {/if}

            {#if rowError && (passwordFor === user.id || emailFor === user.id || nameFor === user.id || confirmingDelete === user.id)}
              <p class="status error">{rowError}</p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if session.auth.local}
      <h2>Add a user</h2>
      <form class="create" onsubmit={submitCreate}>
        <label>
          <span>Email</span>
          <input type="email" bind:value={email} autocomplete="off" autocapitalize="off" required />
        </label>
        <label>
          <span>Password</span>
          <input type="password" bind:value={password} autocomplete="new-password" required />
        </label>
        <label>
          <span>Display name <span class="optional">(optional)</span></span>
          <input type="text" bind:value={displayName} autocomplete="off" />
        </label>
        {#if createError}
          <p class="status error">{createError}</p>
        {/if}
        <button type="submit" disabled={creating}>{creating ? 'Creating…' : 'Create user'}</button>
      </form>
    {/if}
  {:else}
    <p class="blurb">Only an admin can manage users.</p>
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: 1rem;
  }

  .blurb {
    margin: 0 0 1rem;
    max-width: 40rem;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.5;
  }

  h2 {
    margin: 1.75rem 0 0.75rem;
    font-size: 1rem;
    font-weight: 600;
  }

  .users {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
    max-width: 40rem;
  }

  .user {
    padding: 0.75rem 0.9rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .user.skeleton {
    height: 3.1rem;
    animation: skeleton-pulse 1.5s ease-in-out infinite;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .name {
    font-weight: 600;
  }

  .badge {
    padding: 0.05rem 0.4rem;
    border-radius: var(--radius-sm);
    font-size: 0.68rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .badge.admin {
    background: var(--accent-tint);
    color: var(--accent-soft);
  }

  .badge.linked {
    background: rgb(111 208 140 / 0.16);
    color: var(--ok);
  }

  .badge.you {
    background: rgb(255 255 255 / 0.08);
    color: var(--text-dim);
  }

  .joined {
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  .optional {
    opacity: 0.7;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin-left: auto;
  }

  .self-hint,
  .confirm-text {
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  .warn {
    margin: 0.6rem 0 0;
    color: var(--text-dim);
    font-size: 0.82rem;
    line-height: 1.45;
  }

  .hint {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.8rem;
  }

  .panel,
  .create {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .panel {
    margin-top: 0.75rem;
    padding-top: 0.75rem;
    border-top: 1px solid var(--border);
  }

  .create {
    max-width: 22rem;
    padding: 1.25rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
  }

  .panel-actions {
    display: flex;
    gap: 0.5rem;
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

  .status {
    margin: 0.6rem 0 0;
    font-size: 0.85rem;
  }

  .status.error {
    color: var(--err);
  }

  .status.ok {
    margin: 0 0 1rem;
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

  button.ghost {
    padding: 0.35rem 0.6rem;
    background: none;
    border: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.8rem;
    font-weight: 500;
  }

  button.ghost:hover:not(:disabled) {
    background: rgb(255 255 255 / 0.05);
    color: var(--text);
  }

  button.danger-text {
    color: var(--err);
  }

  button.danger {
    padding: 0.35rem 0.7rem;
    background: var(--err);
    color: #1f1e1d;
    font-size: 0.8rem;
  }

  button.danger:hover:not(:disabled) {
    background: #ea9494;
  }
</style>
