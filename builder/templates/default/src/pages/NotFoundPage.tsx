import { Link } from 'react-router-dom'
import { EmptyState } from '../components/ui'

export default function NotFoundPage() {
  return (
    <EmptyState
      title="Page not found"
      description="The address does not match any page of this app."
      action={
        <Link to="/" className="text-primary underline">
          Back to home
        </Link>
      }
    />
  )
}
