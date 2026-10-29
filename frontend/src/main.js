import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'

const app = mount(App, {
  target: /** @type {HTMLElement} */ (document.getElementById('app')),
})

// The service worker only exists in the production build output; registering
// it against the Vite dev server would cache dev-only module URLs. Relative
// path so it works when served under a sub-path.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register('./sw.js').catch(() => {
    // Registration failing (e.g. plain-HTTP deployment) must never break the app.
  })
}

export default app
