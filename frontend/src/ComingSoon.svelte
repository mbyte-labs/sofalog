<script>
  // Landing page for navigation entries whose feature has not been built yet.
  /** @type {{ title: string }} */
  let { title } = $props()
</script>

<section>
  <div class="page-head">
    <h1>{title}</h1>
  </div>
  <p>Not built yet — this page arrives with a later milestone.</p>
</section>

<style>
  .page-head {
    margin-bottom: 0.5rem;
  }

  p {
    margin: 0;
    color: var(--text-dim);
  }
</style>
