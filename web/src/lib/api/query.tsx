import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { toast } from '@/components/ui/toast'
import { ApiError } from './client'

// A session that expires mid-use sends the user to sign in. This goes through the router (registered by main.tsx)
// rather than a full page load: route guards may already be redirecting, and two competing navigations to
// /login (one same-document, one cross-document) wedged the tab.
let unauthorizedHandler: (() => void) | undefined
let redirecting = false
export function setUnauthorizedHandler(fn: () => void) {
  unauthorizedHandler = fn
}
function onUnauthorized() {
  const path = location.pathname
  if (redirecting || path.startsWith('/login') || path.startsWith('/setup')) return
  redirecting = true
  queueMicrotask(() => {
    try {
      unauthorizedHandler?.()
    } finally {
      redirecting = false
    }
  })
}

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15_000,
        refetchOnWindowFocus: false,
        retry: (count, err) => {
          if (err instanceof ApiError && err.status >= 400 && err.status < 500) return false
          return count < 2
        },
      },
    },
    queryCache: new QueryCache({
      onError: (err) => {
        if (err instanceof ApiError && err.isUnauthorized) onUnauthorized()
      },
    }),
    mutationCache: new MutationCache({
      onError: (err, _vars, _ctx, mutation) => {
        if (mutation.options.onError) return // caller handles it
        toast.error(err instanceof ApiError ? err.message : 'Something went wrong')
      },
    }),
  })
}

export function QueryProvider({ children }: { children: ReactNode }) {
  const [client] = useState(createQueryClient)
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
