import { Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'

export function NotFoundScreen() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <EmptyState
        illustration="search"
        title="Page not found"
        description="The page you’re looking for doesn’t exist or you don’t have access to it."
        action={
          <Button variant="primary" asChild>
            <Link to="/">Go home</Link>
          </Button>
        }
      />
    </div>
  )
}

export function ErrorScreen({ error }: { error: Error }) {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <EmptyState
        illustration="error"
        title="Something went wrong"
        description={error.message || 'An unexpected error occurred.'}
        action={
          <Button variant="primary" onClick={() => location.reload()}>
            Reload
          </Button>
        }
      />
    </div>
  )
}
