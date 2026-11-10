import { Link, Outlet, useParams } from '@tanstack/react-router'
import { Settings } from 'lucide-react'
import { settingsSections, type SettingsSection } from '@/app/nav'
import { Screen } from '@/app/screen'
import { themeOptions } from '@/app/theme-toggle'
import { useWorkspace } from '@/app/workspace'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { cn } from '@/lib/cn'
import { useTheme, type ThemePreference } from '@/lib/theme'

export function SettingsLayout() {
  const { org, project } = useWorkspace()
  const { section } = useParams({ strict: false }) as { section?: string }
  return (
    <Screen>
      <PageHeader icon={<Settings />} title="Settings" description={`Manage ${org.name}.`} />
      <div className="grid grid-cols-1 gap-6 md:grid-cols-[200px_1fr]">
        <nav aria-label="Settings" className="flex gap-1 overflow-x-auto md:flex-col">
          {settingsSections.map((s) => (
            <Link
              key={s.id}
              to="/o/$org/p/$project/settings/$section"
              params={{ org: org.slug, project: project?.slug ?? '', section: s.id }}
              className={cn(
                'flex h-8 shrink-0 items-center rounded-md px-2.5 text-sm font-medium transition-colors focus-ring',
                section === s.id ? 'bg-surface text-text shadow-[0_0_0_1px_var(--border)]' : 'text-text-2 hover:bg-surface-hover hover:text-text',
              )}
            >
              {s.label}
            </Link>
          ))}
        </nav>
        <div className="min-w-0">
          <Outlet />
        </div>
      </div>
    </Screen>
  )
}

function AppearanceSettings() {
  const { preference, setPreference } = useTheme()
  return (
    <Card>
      <CardHeader title="Theme" description="Use light, dark, or follow your system setting." />
      <CardBody>
        <SegmentedControl
          aria-label="Theme"
          value={preference}
          onValueChange={(v) => setPreference(v as ThemePreference)}
          options={themeOptions.map((o) => ({ value: o.value, label: o.label, icon: <o.icon /> }))}
        />
      </CardBody>
    </Card>
  )
}

export function SettingsSectionScreen() {
  const { section } = useParams({ strict: false }) as { section: SettingsSection }
  const meta = settingsSections.find((s) => s.id === section)
  if (section === 'appearance') return <AppearanceSettings />
  return (
    <Card>
      <CardHeader title={meta?.label ?? 'Settings'} description={meta?.description} />
      <EmptyState compact illustration="generic" title="Coming with auth & orgs" description="This section is wired up in milestone 2." />
    </Card>
  )
}
