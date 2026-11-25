import { Card } from '../components/ui'

const features = [
  { title: 'First thing', text: 'One short sentence about it.' },
  { title: 'Second thing', text: 'One short sentence about it.' },
  { title: 'Third thing', text: 'One short sentence about it.' },
]

export default function FeaturesSection() {
  return (
    <section id="features" className="border-t border-border bg-surface">
      <div className="mx-auto max-w-5xl px-6 py-16">
        <h2 className="text-2xl font-semibold">What you get</h2>
        <div className="mt-8 grid gap-4 sm:grid-cols-3">
          {features.map((f) => (
            <Card key={f.title}>
              <h3 className="font-semibold">{f.title}</h3>
              <p className="mt-2 text-sm text-muted-foreground">{f.text}</p>
            </Card>
          ))}
        </div>
      </div>
    </section>
  )
}
