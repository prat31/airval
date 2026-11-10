import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { toast } from '@/components/ui/toast'
import { ApiError } from './client'

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
        if (err instanceof ApiError && err.isUnauthorized && !location.pathname.startsWith('/login')) {
          location.assign(`/login?next=${encodeURIComponent(location.pathname)}`)
        }
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
