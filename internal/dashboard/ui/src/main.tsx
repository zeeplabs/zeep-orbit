import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
// Self-hosted web fonts (T0.2/T0.3 dashboard-redesign). Variable axes cover the
// same weight/opsz/FILL/GRAD ranges the Google Fonts CDN used to provide. Imports
// must come before ./index.css so @font-face declarations are parsed first.
import '@fontsource-variable/manrope'
import '@fontsource-variable/space-grotesk'
import '@fontsource-variable/jetbrains-mono'
import '@fontsource-variable/material-symbols-rounded'
import App from './App'
import './lib/i18n'
import './index.css'
import { bootstrapThemeMode } from './lib/theme'

bootstrapThemeMode()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30_000 },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/dashboard">
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
