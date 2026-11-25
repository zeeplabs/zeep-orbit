import { ArrowRight } from 'lucide-react'
import { Button } from '../components/ui'

export default function HeroSection() {
  return (
    <section id="top" className="page mx-auto max-w-5xl px-6 py-20 sm:py-28">
      <h1 className="max-w-2xl text-4xl font-semibold tracking-tight sm:text-5xl">
        A clear promise in one line
      </h1>
      <p className="mt-5 max-w-xl text-lg text-muted-foreground">
        Say who this is for and what they get, in two short sentences.
      </p>
      <a href="#contact" className="mt-8 inline-block no-underline">
        <Button size="md">
          Get in touch <ArrowRight size={16} aria-hidden="true" />
        </Button>
      </a>
    </section>
  )
}
