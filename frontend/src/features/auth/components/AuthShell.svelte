<script>
  // Full-screen centered card for the pre-auth pages (login, first-run
  // setup). These render instead of the app shell, so the layout — brand,
  // backdrop, card — lives here rather than in App.svelte.

  /** @type {{ title: string, children: import('svelte').Snippet }} */
  let { title, children } = $props()
</script>

<div class="screen">
  <div class="card">
    <div class="brand">
      <img src="./icons/logo-mark.svg" alt="" />
      <span>sofalog</span>
    </div>
    <h1>{title}</h1>
    {@render children()}
  </div>
</div>

<style>
  .screen {
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-page);
    padding-bottom: calc(var(--space-page) + env(safe-area-inset-bottom));
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    width: 100%;
    max-width: 22rem;
    padding: 1.5rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-card);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 1.15rem;
    font-weight: 600;
  }

  .brand img {
    width: 1.75rem;
    height: 1.75rem;
    border-radius: var(--radius-sm);
  }

  h1 {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 600;
    letter-spacing: -0.01em;
  }
</style>
