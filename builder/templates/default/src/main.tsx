import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter } from 'react-router-dom'
import App from './App'
import { ToastProvider } from './components/ui'
import './index.css'

const root = document.getElementById('root')
if (!root) throw new Error('Missing #root element')

// The preview is served under a per-session, per-token URL path, so the route
// lives in the hash: it does not depend on the path the app is served from.
createRoot(root).render(
  <StrictMode>
    <HashRouter>
      <ToastProvider>
        <App />
      </ToastProvider>
    </HashRouter>
  </StrictMode>,
)
