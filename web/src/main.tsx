import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './styles/index.css'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { LiveRunsProvider } from './app/live-runs'
import { Toaster } from './components/ui/toast'
import { TooltipProvider } from './components/ui/tooltip'
import { QueryProvider } from './lib/api/query'
import { ThemeProvider } from './lib/theme'
import { router } from './router'

// Dev-only fixture backend (?fixtures=1). The branch and its dynamic import are removed from production builds.
if (import.meta.env.DEV) await (await import('./features/runs/fixtures')).maybeInstallFixtures()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <QueryProvider>
        <TooltipProvider delayDuration={400} skipDelayDuration={200}>
          <LiveRunsProvider>
            <RouterProvider router={router} />
            <Toaster />
          </LiveRunsProvider>
        </TooltipProvider>
      </QueryProvider>
    </ThemeProvider>
  </StrictMode>,
)
