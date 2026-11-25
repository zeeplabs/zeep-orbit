const links = [
  { href: '#features', label: 'Features' },
  { href: '#contact', label: 'Contact' },
]

export default function SiteHeader() {
  return (
    <header className="sticky top-0 z-10 border-b border-border bg-surface/90 backdrop-blur">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-4">
        <a href="#top" className="font-heading text-lg font-semibold text-foreground no-underline">
          Your site
        </a>
        <nav aria-label="Main" className="flex gap-5 text-sm">
          {links.map((l) => (
            <a key={l.href} href={l.href} className="text-muted-foreground no-underline hover:text-foreground">
              {l.label}
            </a>
          ))}
        </nav>
      </div>
    </header>
  )
}
