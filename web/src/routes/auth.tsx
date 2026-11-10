import { Link, Outlet } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import type { FormEvent, ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { toast } from '@/components/ui/toast'

/** Centered, chrome-less layout for /login, /setup and /onboarding. */
export function AuthLayout() {
  return (
    <div className="relative flex min-h-dvh flex-col items-center bg-bg px-4 py-12">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-80 bg-[radial-gradient(ellipse_at_top,var(--accent-subtle),transparent_70%)]"
      />
      <Link to="/" className="relative mb-8 flex items-center gap-2 rounded-md focus-ring" aria-label="airval home">
        <svg viewBox="0 0 24 24" className="size-8" aria-hidden>
          <rect width="24" height="24" rx="6" fill="var(--accent)" />
          <path d="M6.6 17 11.3 7h1.4L17.4 17h-2.3l-1-2.2h-4.2l-1 2.2H6.6Zm4.2-4h2.5L12 10.1 10.8 13Z" fill="var(--on-accent)" />
        </svg>
        <span className="text-lg font-semibold tracking-tight text-text">airval</span>
      </Link>
      <div className="relative w-full">
        <Outlet />
      </div>
    </div>
  )
}

function AuthCard({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-sm rounded-lg border border-border bg-surface p-6 shadow-popover">
      <h1 className="text-lg font-semibold tracking-tight text-text">{title}</h1>
      <p className="mt-1 text-sm text-text-2">{description}</p>
      <div className="mt-6">{children}</div>
    </div>
  )
}

const notYet = (e: FormEvent) => {
  e.preventDefault()
  toast.info('Authentication is wired up in milestone 2.')
}

export function LoginScreen() {
  return (
    <AuthCard title="Sign in" description="Welcome back. Sign in to your airval workspace.">
      <form className="flex flex-col gap-4" onSubmit={notYet}>
        <Field label="Email">
          <Input type="email" autoComplete="email" placeholder="you@company.com" required />
        </Field>
        <Field label="Password">
          <Input type="password" autoComplete="current-password" required />
        </Field>
        <Button type="submit" variant="primary" className="mt-2 w-full">
          Sign in
        </Button>
      </form>
    </AuthCard>
  )
}

export function SetupScreen() {
  return (
    <AuthCard title="Set up airval" description="Create the owner account and your first organization.">
      <form className="flex flex-col gap-4" onSubmit={notYet}>
        <Field label="Your name">
          <Input autoComplete="name" required />
        </Field>
        <Field label="Email">
          <Input type="email" autoComplete="email" required />
        </Field>
        <Field label="Password" hint="At least 12 characters.">
          <Input type="password" autoComplete="new-password" minLength={12} required />
        </Field>
        <Field label="Organization name">
          <Input placeholder="Acme Inc." required />
        </Field>
        <Button type="submit" variant="primary" iconRight={<ArrowRight />} className="mt-2 w-full">
          Create workspace
        </Button>
      </form>
    </AuthCard>
  )
}
