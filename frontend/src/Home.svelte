<script>
  import { api, ApiError } from './lib/api/client.js'

  // M0 placeholder page: proves the shell, the API client and the embed/proxy
  // wiring end to end by calling the one endpoint that exists.

  /**
   * @type {{ state: 'checking' }
   *   | { state: 'up', status: string }
   *   | { state: 'down', detail: string }}
   */
  let server = $state({ state: 'checking' })

  $effect(() => {
    api
      .get('/health')
      .then((data) => {
        server = { state: 'up', status: data.status }
      })
      .catch((err) => {
        server = {
          state: 'down',
          detail: err instanceof ApiError ? err.code : 'unreachable',
        }
      })
  })
</script>

<section>
  <div class="page-head">
    <h1>Welcome to sofalog</h1>
  </div>
  <p class="tagline">Track your movies, shows and games — all on your own server.</p>

  {#if server.state === 'checking'}
    <p class="health">Checking server…</p>
  {:else if server.state === 'up'}
    <p class="health up">Server status: {server.status}</p>
  {:else}
    <p class="health down">Server unreachable ({server.detail})</p>
  {/if}
</section>

<style>
  .page-head {
    margin-bottom: 0.5rem;
  }

  .tagline {
    margin: 0;
    color: var(--text-dim);
  }

  .health {
    display: inline-block;
    margin-top: 1.5rem;
    padding: 0.4rem 0.8rem;
    border-radius: var(--radius-md);
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.9rem;
  }

  .health.up {
    color: var(--ok);
  }

  .health.down {
    color: var(--err);
  }
</style>
