import BackendStatus from '../components/BackendStatus'
import { Card } from '../components/ui'

export default function HomePage() {
  return (
    <section>
      <h1 className="mb-2 text-3xl font-semibold">Welcome</h1>
      <p className="mb-6 text-muted-foreground">This app is ready to be shaped by the AI builder.</p>
      <Card>
        <BackendStatus />
      </Card>
    </section>
  )
}
