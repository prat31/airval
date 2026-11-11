import { Link, Outlet, useMatches, useNavigate, useRouterState } from '@tanstack/react-router'
import {
  BookOpen,
  Building2,
  Check,
  ChevronsUpDown,
  CircleHelp,
  Database,
  Keyboard,
  Layers,
  LogOut,
  Moon,
  PanelLeft,
  Play,
  Plus,
  Search,
  Settings,
  Sparkles,
  Sun,
  User as UserIcon,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Breadcrumbs, type Crumb } from '@/components/ui/breadcrumbs'
import { IconButton } from '@/components/ui/button'
import { CommandPalette, type CommandGroup } from '@/components/ui/command-palette'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Kbd } from '@/components/ui/kbd'
import { Sidebar, SidebarItem, SidebarSection, useSidebar } from '@/components/ui/sidebar'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/cn'
import { modKey, useHotkeys, useMediaQuery } from '@/lib/hotkeys'
import { useTheme, type ThemePreference } from '@/lib/theme'
import { useCrumbNames } from './crumb-names'
import { LiveRunIndicator } from './live-runs'
import { projectNav, settingsNav } from './nav'
import { ShortcutsSheet } from './shortcuts-sheet'
import { themeOptions, ThemeToggle } from './theme-toggle'
import { useLogout } from '@/features/auth/api'
import { NewOrgDialog, NewProjectDialog } from '@/features/auth/workspace-dialogs'
import { initials, useWorkspace, type Project } from './workspace'

const SIDEBAR_KEY = 'airval.sidebar.collapsed'

function useCollapsed() {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      const v = localStorage.getItem(SIDEBAR_KEY)
      if (v != null) return v === '1'
    } catch {
      /* ignore */
    }
    return typeof window !== 'undefined' && window.innerWidth < 1024
  })
  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, collapsed ? '1' : '0')
    } catch {
      /* ignore */
    }
  }, [collapsed])
  return [collapsed, setCollapsed] as const
}

function ProjectDot({ project, className }: { project: Pick<Project, 'color'>; className?: string }) {
  return <span aria-hidden className={cn('size-2 shrink-0 rounded-full', className)} style={{ background: `var(--chart-${project.color})` }} />
}

function Logo({ collapsed }: { collapsed: boolean }) {
  return (
    <span className="flex items-center gap-2">
      <svg viewBox="0 0 24 24" className="size-6 shrink-0" aria-hidden>
        <rect width="24" height="24" rx="6" fill="var(--accent)" />
        <path d="M6.6 17 11.3 7h1.4L17.4 17h-2.3l-1-2.2h-4.2l-1 2.2H6.6Zm4.2-4h2.5L12 10.1 10.8 13Z" fill="var(--on-accent)" />
      </svg>
      {!collapsed && <span className="text-md font-semibold tracking-tight text-text">airval</span>}
    </span>
  )
}

function OrgSwitcher() {
  const { orgs, org } = useWorkspace()
  const { collapsed } = useSidebar()
  const navigate = useNavigate()
  const [newOrg, setNewOrg] = useState(false)
  return (
    <>
    <NewOrgDialog open={newOrg} onOpenChange={setNewOrg} />
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`Organization: ${org.name}`}
          className={cn(
            'flex h-9 items-center gap-2 rounded-md text-left transition-colors hover:bg-surface-hover focus-ring',
            collapsed ? 'size-9 justify-center' : 'w-full px-1.5',
          )}
        >
          <Logo collapsed />
          {!collapsed && (
            <>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-semibold text-text">{org.name}</span>
              </span>
              <ChevronsUpDown className="size-3.5 shrink-0 text-text-muted" />
            </>
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-60">
        <DropdownMenuLabel>Organizations</DropdownMenuLabel>
        {orgs.map((o) => (
          <DropdownMenuItem key={o.slug} icon={<Building2 />} onSelect={() => navigate({ to: '/o/$org', params: { org: o.slug } })}>
            <span className="flex items-center justify-between gap-2">
              {o.name}
              {o.slug === org.slug && <Check className="size-3.5! text-accent!" />}
            </span>
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Plus />} onSelect={() => setNewOrg(true)}>
          Create organization
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    </>
  )
}

function ProjectSwitcher() {
  const { projects, project, org } = useWorkspace()
  const { collapsed } = useSidebar()
  const navigate = useNavigate()
  const [newProject, setNewProject] = useState(false)
  if (!project) return null
  const trigger = (
    <button
      type="button"
      aria-label={`Project: ${project.name}`}
      className={cn(
        'flex h-8 items-center gap-2 rounded-md border border-border bg-surface text-left transition-colors hover:border-border-strong focus-ring',
        collapsed ? 'size-9 justify-center' : 'w-full px-2',
      )}
    >
      <ProjectDot project={project} />
      {!collapsed && (
        <>
          <span className="min-w-0 flex-1 truncate text-sm font-medium text-text">{project.name}</span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-text-muted" />
        </>
      )}
    </button>
  )
  return (
    <>
    <NewProjectDialog open={newProject} onOpenChange={setNewProject} orgId={org.id} orgSlug={org.slug} />
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      <DropdownMenuContent className="w-60" side={collapsed ? 'right' : 'bottom'}>
        <DropdownMenuLabel>Projects in {org.name}</DropdownMenuLabel>
        {projects.map((p) => (
          <DropdownMenuItem
            key={p.slug}
            icon={<ProjectDot project={p} className="mx-1" />}
            onSelect={() => navigate({ to: '/o/$org/p/$project/overview', params: { org: org.slug, project: p.slug } })}
          >
            <span className="flex items-center justify-between gap-2">
              {p.name}
              {p.slug === project.slug && <Check className="size-3.5! text-accent!" />}
            </span>
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Plus />} onSelect={() => setNewProject(true)}>
          New project
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    </>
  )
}

function UserMenu({ onShortcuts }: { onShortcuts: () => void }) {
  const { user, org, project } = useWorkspace()
  const { preference, setPreference } = useTheme()
  const navigate = useNavigate()
  const logout = useLogout()
  return (
    <DropdownMenu>
      <Tooltip content={user.name}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="Account menu"
            className="flex size-7 items-center justify-center rounded-full bg-accent-subtle text-[11px] font-semibold text-accent ring-1 ring-border transition-shadow hover:ring-border-strong focus-ring"
          >
            {initials(user.name)}
          </button>
        </DropdownMenuTrigger>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-60">
        <div className="px-2 pb-2 pt-1.5">
          <div className="truncate text-sm font-medium text-text">{user.name}</div>
          <div className="truncate text-xs text-text-muted">{user.email}</div>
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          icon={<UserIcon />}
          onSelect={() => project && navigate({ to: '/o/$org/p/$project/settings/$section', params: { org: org.slug, project: project.slug, section: 'general' } })}
        >
          Account settings
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger icon={<Sun />}>Theme</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuRadioGroup value={preference} onValueChange={(v) => setPreference(v as ThemePreference)}>
              {themeOptions.map((o) => (
                <DropdownMenuRadioItem key={o.value} value={o.value} icon={<o.icon />}>
                  {o.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuItem icon={<Keyboard />} shortcut="?" onSelect={onShortcuts}>
          Keyboard shortcuts
        </DropdownMenuItem>
        <DropdownMenuItem icon={<BookOpen />} onSelect={() => window.open('https://github.com/airval/airval', '_blank', 'noopener')}>
          Documentation
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<LogOut />} onSelect={() => logout.mutate(undefined, { onSettled: () => navigate({ to: '/login', search: {} }) })}>
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

declare module '@tanstack/react-router' {
  interface StaticDataRouteOption {
    /** Breadcrumb label for this route. Functions receive the route params. */
    crumb?: string | ((params: Record<string, string>) => string)
    /** Collection crumb shown before this one (e.g. Suites › New suite). `path` is relative to the project. */
    parentCrumb?: { label: string; path: string }
  }
}

function useCrumbs(): Crumb[] {
  const matches = useMatches()
  const { org, project } = useWorkspace()
  const names = useCrumbNames(project?.id)
  return useMemo(() => {
    const crumbs: Crumb[] = []
    if (project) crumbs.push({ label: project.name, href: `/o/${org.slug}/p/${project.slug}/overview`, icon: <ProjectDot project={project} /> })
    for (const m of matches) {
      const c = m.staticData?.crumb
      if (!c) continue
      const parent = m.staticData?.parentCrumb
      if (parent && project) crumbs.push({ label: parent.label, href: `/o/${org.slug}/p/${project.slug}/${parent.path}` })
      const entity = (['run', 'suite', 'dataset', 'target'] as const).find((e) => m.routeId.endsWith(`$${e}`))
      const label = (entity && names[entity]) || (typeof c === 'function' ? c(m.params as Record<string, string>) : c)
      if (crumbs.at(-1)?.label === label) continue
      crumbs.push({ label, href: m.pathname })
    }
    return crumbs
  }, [matches, org.slug, project, names.run, names.suite, names.dataset, names.target])
}

function TopBar({ onToggleSidebar, onOpenPalette, onShortcuts }: { onToggleSidebar: () => void; onOpenPalette: () => void; onShortcuts: () => void }) {
  const crumbs = useCrumbs()
  const { org, project } = useWorkspace()
  return (
    <header className="flex h-(--topbar-height) shrink-0 items-center gap-2 border-b border-border bg-bg/80 px-3 backdrop-blur-sm">
      <Tooltip content="Toggle sidebar" shortcut={['[']}>
        <IconButton label="Toggle sidebar" size="sm" onClick={onToggleSidebar}>
          <PanelLeft />
        </IconButton>
      </Tooltip>
      <div className="mx-1 h-4 w-px bg-border" aria-hidden />
      <Breadcrumbs
        items={crumbs}
        className="min-w-0 flex-1"
        renderLink={(c, className, children) => (
          <Link to={c.href!} className={className}>
            {children}
          </Link>
        )}
      />
      <div className="flex shrink-0 items-center gap-1.5">
        {project && <LiveRunIndicator org={org.slug} project={project.slug} />}
        <button
          type="button"
          onClick={onOpenPalette}
          className="hidden h-7 w-56 items-center gap-2 rounded-md border border-border bg-surface pl-2 pr-1 text-sm text-text-muted transition-colors hover:border-border-strong hover:text-text-2 focus-ring md:flex"
        >
          <Search className="size-3.5" />
          <span className="flex-1 truncate whitespace-nowrap text-left">Search or jump to…</span>
          <span className="flex gap-0.5">
            <Kbd>{modKey}</Kbd>
            <Kbd>K</Kbd>
          </span>
        </button>
        <IconButton label="Search" className="md:hidden" onClick={onOpenPalette}>
          <Search />
        </IconButton>
        <ThemeToggle />
        <Tooltip content="Keyboard shortcuts" shortcut={['?']}>
          <IconButton label="Keyboard shortcuts" onClick={onShortcuts}>
            <CircleHelp />
          </IconButton>
        </Tooltip>
        <div className="ml-1">
          <UserMenu onShortcuts={onShortcuts} />
        </div>
      </div>
    </header>
  )
}

/** Authenticated, project-scoped application frame. */
export function AppShell() {
  const [collapsed, setCollapsed] = useCollapsed()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const { org, project, projects } = useWorkspace()
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { resolved, setPreference } = useTheme()
  const isMobile = useMediaQuery('(max-width: 767px)')
  // The off-canvas sidebar is open only for the path it was opened on, so navigating closes it.
  const [mobileOpenAt, setMobileOpenAt] = useState<string | null>(null)
  const mobileOpen = mobileOpenAt === pathname
  const setMobileOpen = (open: boolean) => setMobileOpenAt(open ? pathname : null)
  const toggleSidebar = () => (isMobile ? setMobileOpen(!mobileOpen) : setCollapsed((c) => !c))
  const railCollapsed = isMobile ? false : collapsed

  const params = { org: org.slug, project: project?.slug ?? '' }
  const go = (to: string) => navigate({ to, params })

  const hotkeys: Record<string, () => void> = {
    'mod+k': () => setPaletteOpen((o) => !o),
    '?': () => setShortcutsOpen(true),
    '[': toggleSidebar,
  }
  for (const n of [...projectNav, settingsNav]) hotkeys[`g ${n.key}`] = () => go(n.to)
  useHotkeys(hotkeys)

  const groups: CommandGroup[] = useMemo(
    () => [
      {
        heading: 'Go to',
        items: [...projectNav, settingsNav].map((n) => ({
          id: `nav-${n.id}`,
          label: n.label,
          icon: <n.icon />,
          shortcut: ['G', n.key.toUpperCase()],
          onSelect: () => navigate({ to: n.to, params }),
        })),
      },
      {
        heading: 'Actions',
        items: [
          { id: 'run-suite', label: 'Run suite…', icon: <Play />, keywords: ['start', 'execute'], onSelect: () => navigate({ to: '/o/$org/p/$project/runs', params, search: {} }) },
          { id: 'new-suite', label: 'New suite…', icon: <Layers />, onSelect: () => navigate({ to: '/o/$org/p/$project/suites/new', params }) },
          { id: 'new-dataset', label: 'New dataset…', icon: <Database />, keywords: ['import', 'upload'], onSelect: () => navigate({ to: '/o/$org/p/$project/datasets', params }) },
          { id: 'new-project', label: 'New project…', icon: <Plus />, onSelect: () => navigate({ to: '/onboarding' }) },
          {
            id: 'toggle-theme',
            label: resolved === 'dark' ? 'Switch to light theme' : 'Switch to dark theme',
            icon: resolved === 'dark' ? <Sun /> : <Moon />,
            keywords: ['theme', 'appearance', 'dark', 'light'],
            onSelect: () => setPreference(resolved === 'dark' ? 'light' : 'dark'),
          },
          { id: 'shortcuts', label: 'Keyboard shortcuts', icon: <Keyboard />, shortcut: ['?'], onSelect: () => setShortcutsOpen(true) },
        ],
      },
      {
        heading: 'Projects',
        items: projects.map((p) => ({
          id: `project-${p.slug}`,
          label: p.name,
          hint: p.slug === project?.slug ? 'Current' : undefined,
          icon: <ProjectDot project={p} className="mx-1" />,
          onSelect: () => navigate({ to: '/o/$org/p/$project/overview', params: { org: org.slug, project: p.slug } }),
        })),
      },
      ...(import.meta.env.DEV
        ? [{ heading: 'Developer', items: [{ id: 'kitchen-sink', label: 'Kitchen sink', icon: <Sparkles />, onSelect: () => navigate({ to: '/__kitchen-sink' }) }] }]
        : []),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [org.slug, project?.slug, projects, resolved],
  )

  return (
    <div className="flex h-dvh overflow-hidden bg-bg">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:bg-surface focus:px-3 focus:py-2 focus:text-sm focus:shadow-popover"
      >
        Skip to content
      </a>
      {isMobile && mobileOpen && (
        <button type="button" aria-label="Close navigation" className="fixed inset-0 z-30 bg-overlay animate-fade" onClick={() => setMobileOpen(false)} />
      )}
      <Sidebar
        collapsed={railCollapsed}
        className={cn(
          isMobile && 'fixed inset-y-0 left-0 z-40 shadow-dialog transition-transform duration-200 ease-out',
          isMobile && !mobileOpen && '-translate-x-full',
        )}
        aria-hidden={isMobile && !mobileOpen ? true : undefined}
        inert={isMobile && !mobileOpen ? true : undefined}
        header={
          <>
            <OrgSwitcher />
            <div className="mt-1 w-full">
              <ProjectSwitcher />
            </div>
          </>
        }
        footer={
          <SidebarItem
            asChild
            icon={<Settings />}
            label="Settings"
            shortcut={['G', ',']}
            active={pathname.includes('/settings')}
          >
            <Link to="/o/$org/p/$project/settings" params={params}>
              <Settings />
              {!railCollapsed && <span className="flex-1 truncate">Settings</span>}
            </Link>
          </SidebarItem>
        }
      >
        <SidebarSection>
          {projectNav.map((n) => {
            const active = pathname.includes(`/p/${params.project}/${n.id}`)
            return (
              <SidebarItem key={n.id} asChild icon={<n.icon />} label={n.label} active={active} shortcut={['G', n.key.toUpperCase()]}>
                <Link to={n.to} params={params}>
                  <n.icon />
                  {!railCollapsed && <span className="flex-1 truncate">{n.label}</span>}
                </Link>
              </SidebarItem>
            )
          })}
        </SidebarSection>
      </Sidebar>
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar onToggleSidebar={toggleSidebar} onOpenPalette={() => setPaletteOpen(true)} onShortcuts={() => setShortcutsOpen(true)} />
        <main id="main" className="min-h-0 flex-1 overflow-y-auto" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} groups={groups} />
      <ShortcutsSheet open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </div>
  )
}
