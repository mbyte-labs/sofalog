<script>
  import { route } from '@mateothegreat/svelte5-router'
</script>

<section>
  <h1>Page not found</h1>
  <p>There is nothing at this address.</p>
  <a href="/" use:route>Back to home</a>
</section>

<style>
  h1 {
    margin: 0 0 0.5rem;
  }

  a {
    color: var(--accent);
  }
</style>
