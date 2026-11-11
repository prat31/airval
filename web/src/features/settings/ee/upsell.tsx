import { ArrowUpRight, Check, Sparkles } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

export interface UpsellCopy {
  title: string
  pitch: string
  points: string[]
}

/** Shown in community builds in place of an enterprise settings page. */
export function EnterpriseUpsell({ copy }: { copy: UpsellCopy }) {
  return (
    <Card className="overflow-hidden">
      <div className="relative px-6 py-8 sm:px-8">
        <div aria-hidden className="pointer-events-none absolute inset-x-0 top-0 h-40 bg-[radial-gradient(ellipse_at_top_right,var(--accent-subtle),transparent_70%)]" />
        <div className="relative flex max-w-xl flex-col gap-4">
          <div className="flex items-center gap-2">
            <span className="flex size-8 items-center justify-center rounded-md bg-accent-subtle text-accent">
              <Sparkles className="size-4" aria-hidden />
            </span>
            <Badge variant="accent">Enterprise</Badge>
          </div>
          <div>
            <h2 className="text-lg font-semibold tracking-tight text-text">{copy.title}</h2>
            <p className="mt-1 text-sm text-text-2">{copy.pitch}</p>
          </div>
          <ul className="flex flex-col gap-2">
            {copy.points.map((p) => (
              <li key={p} className="flex items-start gap-2 text-sm text-text-2">
                <Check className="mt-0.5 size-4 shrink-0 text-accent" aria-hidden />
                <span>{p}</span>
              </li>
            ))}
          </ul>
          <p className="text-xs text-text-muted">
            Enterprise features ship in the same binary built with <code className="font-mono">-tags ee</code> and are licensed under LICENSE-EE.
          </p>
          <div>
            <Button variant="primary" iconRight={<ArrowUpRight />} onClick={() => window.open('https://github.com/prat31/airval#enterprise', '_blank', 'noopener')}>
              Learn about Enterprise
            </Button>
          </div>
        </div>
      </div>
    </Card>
  )
}

export const upsellCopy = {
  sso: {
    title: 'Single sign-on',
    pitch: 'Let your team sign in with your identity provider instead of passwords.',
    points: ['OIDC (Okta, Entra ID, Google Workspace, Auth0…) with PKCE', 'SAML 2.0 service provider with signed assertions', 'Just-in-time provisioning and group → role mapping', 'Restrict sign-in to your email domains'],
  },
  scim: {
    title: 'SCIM provisioning',
    pitch: 'Create, update and deactivate members automatically from your directory.',
    points: ['SCIM 2.0 Users and Groups', 'Deactivation signs people out immediately', 'Map directory groups to roles'],
  },
  roles: {
    title: 'Custom roles',
    pitch: 'Fine-grained permissions and per-project access on top of the four built-in roles.',
    points: ['Build roles from permissions like “runs: execute” or “datasets: write”', 'Give people a different role in a single project', 'Organization administration always stays with owners and admins'],
  },
  'audit-log': {
    title: 'Audit log',
    pitch: 'A complete record of who changed what, from where.',
    points: ['Every mutating API call with actor, IP and user agent', 'Filter by person, action and time; export to CSV', 'Configurable retention'],
  },
  retention: {
    title: 'Data retention',
    pitch: 'Keep your database lean and meet your data policies.',
    points: ['Delete runs and results older than N days, per project', 'Pinned baselines are kept', 'Applied automatically every hour'],
  },
  branding: {
    title: 'Branding',
    pitch: 'Make airval feel like part of your platform.',
    points: ['Your product name and logo in the app', 'Choose an accent from a curated, accessible palette'],
  },
} satisfies Record<string, UpsellCopy>
