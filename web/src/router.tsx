/**
 * Route tree (code-based TanStack Router).
 *
 * Conventions
 *  - Everything project-scoped lives under /o/$org/p/$project/… and renders inside <AppShell>.
 *  - Each route sets `staticData.crumb` for the top-bar breadcrumbs.
 *  - Screens live in src/routes/*; this file only wires paths → components.
 *  - URL holds view state: use `validateSearch` for filters/tabs/selection.
 */
import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet, redirect } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { EntryRedirect, OrgIndexRedirect, WorkspaceGate } from './features/auth/guard'
import { OnboardingScreen } from './features/auth/onboarding'
import { AuthLayout, LoginScreen, SetupScreen } from './features/auth/screens'
import { CompareScreen } from './features/compare/compare'
import { validateCompareSearch } from './features/compare/search'
import { OverviewScreen } from './features/overview/overview'
import { RunDetailScreen } from './features/runs/run-detail'
import { RunsScreen } from './features/runs/runs-list'
import { shortId } from './features/runs/ui'
import { validateRunDetailSearch, validateRunsSearch } from './features/runs/search'
import { ErrorScreen, NotFoundScreen } from './routes/not-found'
import {
  DatasetDetailScreen,
  DatasetsScreen,
  EvaluatorsScreen,
  ReviewScreen,
  SuiteDetailScreen,
  SuitesScreen,
  TargetDetailScreen,
  TargetsScreen,
} from './routes/placeholders'
import { SettingsLayout, SettingsSectionScreen } from './routes/settings'
import { SuiteNewScreen } from './routes/suite-new'

const rootRoute = createRootRoute({
  component: Outlet,
  notFoundComponent: NotFoundScreen,
  errorComponent: ({ error }) => <ErrorScreen error={error instanceof Error ? error : new Error(String(error))} />,
})

// Resolves setup → session → first org → first project.
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: EntryRedirect })

// Chrome-less auth / onboarding.
const authLayout = createRoute({ getParentRoute: () => rootRoute, id: 'auth', component: AuthLayout })
const loginRoute = createRoute({
  getParentRoute: () => authLayout,
  path: '/login',
  component: LoginScreen,
  validateSearch: (s: Record<string, unknown>): { next?: string } => (typeof s.next === 'string' ? { next: s.next } : {}),
})
const setupRoute = createRoute({ getParentRoute: () => authLayout, path: '/setup', component: SetupScreen })
const onboardingRoute = createRoute({
  getParentRoute: () => authLayout,
  path: '/onboarding',
  component: OnboardingScreen,
  validateSearch: (s: Record<string, unknown>): { org?: string } => (typeof s.org === 'string' ? { org: s.org } : {}),
})

// Organization → first project.
const orgRoute = createRoute({ getParentRoute: () => rootRoute, path: '/o/$org', component: Outlet })
const orgIndex = createRoute({ getParentRoute: () => orgRoute, path: '/', component: OrgIndexRedirect })

// Project shell (rendered once session, org and project are resolved).
const projectRoute = createRoute({ getParentRoute: () => orgRoute, path: 'p/$project', component: WorkspaceGate })
const projectIndex = createRoute({
  getParentRoute: () => projectRoute,
  path: '/',
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/o/$org/p/$project/overview', params })
  },
})

type Crumb = string | ((p: Record<string, string>) => string)
const route = <TPath extends string>(path: TPath, component: () => ReactNode, crumb?: Crumb, parentCrumb?: { label: string; path: string }) =>
  createRoute({ getParentRoute: () => projectRoute, path, component, staticData: { crumb, parentCrumb } })

const overviewRoute = route('overview', OverviewScreen, 'Overview')
const runsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'runs',
  component: RunsScreen,
  staticData: { crumb: 'Runs' },
  validateSearch: validateRunsSearch,
})
const runRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'runs/$run',
  component: RunDetailScreen,
  staticData: { crumb: (p) => (p.run ? `Run ${shortId(p.run)}` : 'Run'), parentCrumb: { label: 'Runs', path: 'runs' } },
  validateSearch: validateRunDetailSearch,
})
const compareRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'compare',
  component: CompareScreen,
  staticData: { crumb: 'Compare', parentCrumb: { label: 'Runs', path: 'runs' } },
  validateSearch: validateCompareSearch,
})
const suitesRoute = route('suites', SuitesScreen, 'Suites')
const suiteNewRoute = route('suites/new', SuiteNewScreen, 'New suite', { label: 'Suites', path: 'suites' })
const suiteRoute = route('suites/$suite', SuiteDetailScreen, (p) => p.suite ?? 'Suite', { label: 'Suites', path: 'suites' })
const datasetsRoute = route('datasets', DatasetsScreen, 'Datasets')
const datasetRoute = route('datasets/$dataset', DatasetDetailScreen, (p) => p.dataset ?? 'Dataset', { label: 'Datasets', path: 'datasets' })
const targetsRoute = route('targets', TargetsScreen, 'Targets')
const targetRoute = route('targets/$target', TargetDetailScreen, (p) => p.target ?? 'Target', { label: 'Targets', path: 'targets' })
const evaluatorsRoute = route('evaluators', EvaluatorsScreen, 'Evaluators')
const reviewRoute = route('review', ReviewScreen, 'Review')

const settingsRoute = route('settings', SettingsLayout, 'Settings')
const settingsIndex = createRoute({
  getParentRoute: () => settingsRoute,
  path: '/',
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/o/$org/p/$project/settings/$section', params: { ...params, section: 'general' } })
  },
})
const settingsSectionRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: '$section',
  component: SettingsSectionScreen,
  staticData: { crumb: (p) => (p.section ?? '').replace(/-/g, ' ').replace(/^\w/, (c) => c.toUpperCase()) },
})

// Dev-only component showcase. The lazy import is dead-code-eliminated from production builds.
const kitchenSinkRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/__kitchen-sink',
  component: import.meta.env.DEV ? lazyRouteComponent(() => import('./kitchen-sink/kitchen-sink'), 'KitchenSink') : NotFoundScreen,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  authLayout.addChildren([loginRoute, setupRoute, onboardingRoute]),
  orgRoute.addChildren([
    orgIndex,
    projectRoute.addChildren([
      projectIndex,
      overviewRoute,
      runsRoute,
      runRoute,
      compareRoute,
      suitesRoute,
      suiteNewRoute,
      suiteRoute,
      datasetsRoute,
      datasetRoute,
      targetsRoute,
      targetRoute,
      evaluatorsRoute,
      reviewRoute,
      settingsRoute.addChildren([settingsIndex, settingsSectionRoute]),
    ]),
  ]),
  kitchenSinkRoute,
])

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  scrollRestoration: true,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
