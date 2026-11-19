import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// `base` is intentionally left at the Vite default ("/"). The builder
// overrides it for the preview build with `vite build --base ./` (a relative
// base, so assets resolve under the per-token preview path); the published
// static site is served from the domain root.
export default defineConfig({
  plugins: [react()],
})
