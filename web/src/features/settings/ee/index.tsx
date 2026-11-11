import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useEdition, type EnterpriseFeature } from '@/lib/edition'
import { AuditSettingsPage } from './audit'
import { BrandingSettings } from './branding'
import { RetentionSettings } from './retention'
import { RolesSettings } from './roles'
import { SCIMSettings } from './scim'
import { SSOSettings } from './sso'
import { EnterpriseUpsell, upsellCopy } from './upsell'

/** Enterprise settings sections, in nav order. */
export const eeSections = [
  { id: 'sso', label: 'Single sign-on', feature: 'sso' },
  { id: 'scim', label: 'SCIM', feature: 'scim' },
  { id: 'roles', label: 'Roles', feature: 'custom_roles' },
  { id: 'audit-log', label: 'Audit log', feature: 'audit_log' },
  { id: 'retention', label: 'Data retention', feature: 'retention' },
  { id: 'branding', label: 'Branding', feature: 'branding' },
] as const satisfies readonly { id: string; label: string; feature: EnterpriseFeature }[]

export type EESection = (typeof eeSections)[number]['id']

export const isEESection = (id: string): id is EESection => eeSections.some((s) => s.id === id)

const screens: Record<EESection, () => React.ReactNode> = {
  sso: () => <SSOSettings />,
  scim: () => <SCIMSettings />,
  roles: () => <RolesSettings />,
  'audit-log': () => <AuditSettingsPage />,
  retention: () => <RetentionSettings />,
  branding: () => <BrandingSettings />,
}

/** Renders an enterprise section, or the upsell card when the running edition lacks the feature. */
export function EESectionScreen({ section }: { section: EESection }) {
  const { isLoading, has } = useEdition()
  const def = eeSections.find((s) => s.id === section)!
  if (isLoading) return <Card className="p-4"><Skeleton className="h-32 w-full" /></Card>
  if (!has(def.feature)) return <EnterpriseUpsell copy={upsellCopy[section]} />
  return <>{screens[section]()}</>
}
