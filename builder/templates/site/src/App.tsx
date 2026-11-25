import ContactSection from './sections/ContactSection'
import FeaturesSection from './sections/FeaturesSection'
import HeroSection from './sections/HeroSection'
import SiteHeader from './sections/SiteHeader'

// One page, several sections. Each section has an id so the header links
// (`href="#features"`) scroll to it; there is no router.
export default function App() {
  return (
    <div className="min-h-screen">
      <SiteHeader />
      <main>
        <HeroSection />
        <FeaturesSection />
        <ContactSection />
      </main>
      <footer className="border-t border-border px-6 py-8 text-center text-sm text-muted-foreground">
        © {new Date().getFullYear()} Your site
      </footer>
    </div>
  )
}
