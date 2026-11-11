import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, Outlet, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowRight, Building2, CircleAlert, KeyRound, Lock, Mail, User } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError } from '@/lib/api/client'
import { authProvidersQuery } from '@/lib/edition'
import { setupStatusQuery, slugify, useLogin, useSetup } from './api'

export function Logo({ size = 8 }: { size?: 6 | 8 }) {
  return (
    <svg viewBox="0 0 24 24" className={size === 8 ? 'size-8' : 'size-6'} aria-hidden>
      <rect width="24" height="24" rx="6" fill="var(--accent)" />
      <path d="M6.6 17 11.3 7h1.4L17.4 17h-2.3l-1-2.2h-4.2l-1 2.2H6.6Zm4.2-4h2.5L12 10.1 10.8 13Z" fill="var(--on-accent)" />
    </svg>
  )
}

/** Centered, chrome-less layout for /login, /setup and /onboarding. */
export function AuthLayout() {
  return (
    <div className="relative flex min-h-dvh flex-col items-center bg-bg px-4 py-12 sm:py-16">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-96 bg-[radial-gradient(ellipse_at_top,var(--accent-subtle),transparent_70%)]"
      />
      <Link to="/" className="relative mb-8 flex items-center gap-2 rounded-md focus-ring" aria-label="airval home">
        <Logo />
        <span className="text-lg font-semibold tracking-tight text-text">airval</span>
      </Link>
      <div className="relative w-full">
        <Outlet />
      </div>
      <p className="relative mt-auto pt-10 text-xs text-text-muted">Self-hosted evaluation for AI services</p>
    </div>
  )
}

export function AuthCard({ title, description, children, footer }: { title: string; description: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-sm">
      <div className="rounded-lg border border-border bg-surface p-6 shadow-popover sm:p-7">
        <h1 className="text-lg font-semibold tracking-tight text-text">{title}</h1>
        <p className="mt-1 text-sm text-text-2">{description}</p>
        <div className="mt-6">{children}</div>
      </div>
      {footer && <div className="mt-4 text-center text-sm text-text-muted">{footer}</div>}
    </div>
  )
}

function FormError({ error }: { error: unknown }) {
  if (!error) return null
  const msg =
    error instanceof ApiError
      ? error.status === 401
        ? 'That email and password don’t match an account.'
        : error.message
      : 'Something went wrong. Try again.'
  return (
    <div role="alert" className="flex items-start gap-2 rounded-md bg-fail-subtle px-3 py-2 text-sm text-fail">
      <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
      <span>{msg}</span>
    </div>
  )
}

/** Only same-origin, path-only redirects are honored. */
const safeNext = (next?: string) => (next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/login') ? next : '/')

const ssoErrors: Record<string, string> = {
  not_provisioned: 'Your account isn’t set up in this workspace yet. Ask an admin to invite you.',
  domain_not_allowed: 'Your email domain isn’t allowed for this sign-in method.',
  email_unverified: 'Your identity provider hasn’t verified your email address.',
  no_email: 'Your identity provider didn’t share an email address.',
  deactivated: 'Your account has been deactivated.',
  state_expired: 'Sign-in took too long. Please try again.',
  state_missing: 'Sign-in session expired. Please try again.',
  unknown_provider: 'That sign-in method is no longer available.',
  provider_unavailable: 'We couldn’t reach your identity provider. Try again shortly.',
}

function SSOButtons({ next }: { next?: string }) {
  const providers = useQuery(authProvidersQuery)
  if (!providers.data?.length) return null
  return (
    <div className="flex flex-col gap-2">
      {providers.data.map((p) => (
        <Button key={p.id} asChild variant="secondary" className="w-full">
          <a href={`${p.start_url}?redirect=${encodeURIComponent(safeNext(next))}`}>
            <KeyRound aria-hidden />
            Continue with {p.name}
          </a>
        </Button>
      ))}
      <div className="my-2 flex items-center gap-3 text-xs text-text-muted" role="separator">
        <span className="h-px flex-1 bg-border" />
        or use your password
        <span className="h-px flex-1 bg-border" />
      </div>
    </div>
  )
}

export function LoginScreen() {
  const { next, sso_error } = useSearch({ strict: false }) as { next?: string; sso_error?: string }
  const setup = useQuery(setupStatusQuery)
  const login = useLogin()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  if (setup.data?.needs_setup) return <Navigate to="/setup" replace />

  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate({ email: email.trim(), password }, { onSuccess: () => navigate({ to: safeNext(next), replace: true }) })
  }

  return (
    <AuthCard title="Sign in" description="Welcome back. Sign in to your airval workspace.">
      {sso_error && !login.error && (
        <div role="alert" className="mb-4 flex items-start gap-2 rounded-md bg-fail-subtle px-3 py-2 text-sm text-fail">
          <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
          <span>{ssoErrors[sso_error] ?? 'Single sign-on failed. Try again or contact your admin.'}</span>
        </div>
      )}
      <SSOButtons next={next} />
      <form className="flex flex-col gap-4" onSubmit={submit} noValidate={false}>
        <FormError error={login.error} />
        <Field label="Email">
          <Input
            type="email"
            autoComplete="email"
            placeholder="you@company.com"
            leading={<Mail />}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoFocus
          />
        </Field>
        <Field label="Password">
          <Input
            type="password"
            autoComplete="current-password"
            leading={<Lock />}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        <Button type="submit" variant="primary" className="mt-2 w-full" loading={login.isPending}>
          Sign in
        </Button>
      </form>
    </AuthCard>
  )
}

export function SetupScreen() {
  const setup = useQuery(setupStatusQuery)
  const run = useSetup()
  const navigate = useNavigate()
  const [form, setForm] = useState({ name: '', email: '', password: '', org: '' })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }))

  if (setup.isPending) {
    return (
      <div className="mx-auto w-full max-w-sm rounded-lg border border-border bg-surface p-7">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="mt-2 h-4 w-64" />
        <div className="mt-6 flex flex-col gap-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      </div>
    )
  }
  if (setup.data && !setup.data.needs_setup && !run.isSuccess) return <Navigate to="/login" replace />

  const tooShort = form.password.length > 0 && form.password.length < 8
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (tooShort) return
    run.mutate(
      { name: form.name.trim(), email: form.email.trim(), password: form.password, org_name: form.org.trim(), org_slug: slugify(form.org) || undefined },
      { onSuccess: () => navigate({ to: '/onboarding', replace: true }) },
    )
  }

  return (
    <AuthCard
      title="Set up airval"
      description="Create the owner account and your first organization. You can invite your team afterwards."
    >
      <form className="flex flex-col gap-4" onSubmit={submit}>
        <FormError error={run.error} />
        <Field label="Your name">
          <Input autoComplete="name" leading={<User />} value={form.name} onChange={set('name')} placeholder="Ada Lovelace" required autoFocus />
        </Field>
        <Field label="Email">
          <Input type="email" autoComplete="email" leading={<Mail />} value={form.email} onChange={set('email')} placeholder="you@company.com" required />
        </Field>
        <Field label="Password" hint="At least 8 characters." error={tooShort ? 'Use at least 8 characters.' : undefined}>
          <Input type="password" autoComplete="new-password" leading={<Lock />} value={form.password} onChange={set('password')} minLength={8} required />
        </Field>
        <div className="my-1 h-px bg-border" aria-hidden />
        <Field label="Organization" hint={form.org ? <span className="font-mono">/{slugify(form.org) || '…'}</span> : 'Usually your company or team name.'}>
          <Input leading={<Building2 />} value={form.org} onChange={set('org')} placeholder="Acme Inc." required />
        </Field>
        <Button type="submit" variant="primary" iconRight={<ArrowRight />} className="mt-2 w-full" loading={run.isPending}>
          Create workspace
        </Button>
      </form>
    </AuthCard>
  )
}
