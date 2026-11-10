import {
  ClipboardCheck,
  Crosshair,
  Database,
  Gauge,
  Layers,
  LayoutDashboard,
  Play,
  Settings,
  type LucideIcon,
} from 'lucide-react'

/** Project-scoped sections, in sidebar order. `key` is the `g <key>` shortcut. */
export const projectNav = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard, to: '/o/$org/p/$project/overview', key: 'o' },
  { id: 'runs', label: 'Runs', icon: Play, to: '/o/$org/p/$project/runs', key: 'r' },
  { id: 'suites', label: 'Suites', icon: Layers, to: '/o/$org/p/$project/suites', key: 's' },
  { id: 'datasets', label: 'Datasets', icon: Database, to: '/o/$org/p/$project/datasets', key: 'd' },
  { id: 'targets', label: 'Targets', icon: Crosshair, to: '/o/$org/p/$project/targets', key: 't' },
  { id: 'evaluators', label: 'Evaluators', icon: Gauge, to: '/o/$org/p/$project/evaluators', key: 'e' },
  { id: 'review', label: 'Review', icon: ClipboardCheck, to: '/o/$org/p/$project/review', key: 'v' },
] as const satisfies readonly { id: string; label: string; icon: LucideIcon; to: string; key: string }[]

export const settingsNav = { id: 'settings', label: 'Settings', icon: Settings, to: '/o/$org/p/$project/settings', key: ',' } as const

export const settingsSections = [
  { id: 'general', label: 'General', description: 'Organization name, slug and defaults.' },
  { id: 'members', label: 'Members', description: 'Invite people and manage roles.' },
  { id: 'api-keys', label: 'API keys', description: 'Keys for CI and the airval CLI.' },
  { id: 'secrets', label: 'Secrets', description: 'Encrypted credentials referenced by targets and judges.' },
  { id: 'notifications', label: 'Notifications', description: 'Slack, webhooks and email on run finish or gate failure.' },
  { id: 'appearance', label: 'Appearance', description: 'Theme and display preferences.' },
] as const

export type SettingsSection = (typeof settingsSections)[number]['id']
