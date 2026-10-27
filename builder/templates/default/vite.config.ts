import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// `base` is intentionally left at the Vite default ("/"). The builder
// overrides it per preview session with `vite build --base <preview_base>`;
// the published static site is served from the domain root.
export default defineConfig({
  plugins: [react()],
})
