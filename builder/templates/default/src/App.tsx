import { Link, Route, Routes } from 'react-router-dom'
import HomePage from './pages/HomePage'
import NotFoundPage from './pages/NotFoundPage'

export default function App() {
  return (
    <div className="min-h-screen">
      <header className="border-b border-border bg-surface px-6 py-4">
        <Link to="/" className="font-semibold text-foreground no-underline">
          Orbit Frontend App
        </Link>
      </header>
      <main className="page mx-auto max-w-3xl px-6 py-8">
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </main>
    </div>
  )
}
