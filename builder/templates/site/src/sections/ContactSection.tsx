import { Mail } from 'lucide-react'

// A static site has no server to receive a form. Contact is a link the visitor
// opens in their own mail app or messenger.
export default function ContactSection() {
  return (
    <section id="contact" className="mx-auto max-w-5xl px-6 py-16">
      <h2 className="text-2xl font-semibold">Contact</h2>
      <p className="mt-3 max-w-xl text-muted-foreground">Tell visitors the one way to reach you.</p>
      <a
        href="mailto:hello@example.com"
        className="mt-5 inline-flex items-center gap-2 font-medium text-primary underline-offset-4 hover:underline"
      >
        <Mail size={16} aria-hidden="true" /> hello@example.com
      </a>
    </section>
  )
}
