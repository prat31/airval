import { Calendar, KeyRound, Plus, RefreshCw, Trash2, Webhook } from 'lucide-react'
import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { CodeBlock, CopyButton } from '@/components/ui/code-block'
import { CronInput } from '@/components/ui/cron-input'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { errorMessage, type S } from '../shared/api'
import { ConfirmDialog, ErrorPanel, KvEditor, RelTime } from '../shared/ui'
import { useTriggers, useWebhookMutations } from './api'

const absolute = (u: string) => (u.startsWith('http') ? u : `${window.location.origin}${u}`)

export function webhookCurl(url: string, secret = '$AIRVAL_WEBHOOK_SECRET') {
  return `BODY='{"labels":{"git_sha":"abc123"}}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "${secret}" | sed 's/^.* //')
curl -X POST '${absolute(url)}' \\
  -H "Content-Type: application/json" \\
  -H "X-Airval-Timestamp: $TS" \\
  -H "X-Airval-Signature: sha256=$SIG" \\
  -d "$BODY"`
}

function SecretReveal({ trigger, onClose }: { trigger: S['TriggerCreated'] | null; onClose: () => void }) {
  return (
    <Dialog
      open={!!trigger}
      onOpenChange={(o) => !o && onClose()}
      title="Webhook secret"
      description="Copy it now. For security it is shown only once and can't be retrieved later (you can rotate it)."
      size="md"
      footer={<Button variant="primary" onClick={onClose}>I've saved it</Button>}
    >
      {trigger && (
        <div className="flex flex-col gap-4">
          <Field label="Signing secret">
            <div className="flex items-center gap-2 rounded-sm border border-border bg-surface-2 px-2.5 py-1.5">
              <code className="min-w-0 flex-1 truncate font-mono text-xs text-text">{trigger.webhook_secret}</code>
              <CopyButton value={trigger.webhook_secret ?? ''} label="Copy secret" />
            </div>
          </Field>
          <Field label="Sample request">
            <CodeBlock code={webhookCurl(trigger.webhook_url ?? '', trigger.webhook_secret)} language="bash" maxHeight={220} />
          </Field>
        </div>
      )}
    </Dialog>
  )
}

function AddDialog({ suiteId, open, onOpenChange, onCreated }: { suiteId: string; open: boolean; onOpenChange: (o: boolean) => void; onCreated: (t: S['TriggerCreated']) => void }) {
  const { create } = useWebhookMutations(suiteId)
  const [kind, setKind] = useState<'schedule' | 'webhook'>('schedule')
  const [name, setName] = useState('')
  const [cron, setCron] = useState('0 6 * * *')
  const [tz, setTz] = useState('UTC')
  const [vars, setVars] = useState<Record<string, string>>({})
  const [labels, setLabels] = useState<Record<string, string>>({ git_sha: '$.after', branch: '$.ref' })
  const submit = () => {
    const config: Record<string, unknown> = kind === 'schedule' ? { cron, timezone: tz } : { payload_variables: vars, payload_labels: labels }
    create.mutate(
      { kind, name: name.trim() || (kind === 'schedule' ? 'Schedule' : 'Webhook'), config: config as S['TriggerConfig'], enabled: true },
      {
        onSuccess: (t) => {
          onOpenChange(false)
          setName('')
          if (kind === 'webhook') onCreated(t)
          else toast.success('Schedule added')
        },
        onError: (e) => toast.error(errorMessage(e)),
      },
    )
  }
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title="Add trigger"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button variant="primary" loading={create.isPending} onClick={submit}>Add trigger</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <SegmentedControl aria-label="Trigger type" value={kind} onValueChange={setKind} options={[{ value: 'schedule', label: 'Schedule', icon: <Calendar /> }, { value: 'webhook', label: 'Webhook', icon: <Webhook /> }]} />
        <Field label="Name" optional>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === 'schedule' ? 'Nightly' : 'Deploy preview'} />
        </Field>
        {kind === 'schedule' ? (
          <>
            <Field label="Cron">
              <CronInput value={cron} onChange={setCron} timezone={tz} />
            </Field>
            <Field label="Timezone" hint="IANA name, e.g. America/New_York">
              <Input value={tz} onChange={(e) => setTz(e.target.value)} />
            </Field>
          </>
        ) : (
          <>
            <Field label="Variables from payload" optional hint="Variable name → JSONPath into the request body, e.g. a deploy preview URL.">
              <KvEditor value={vars} onChange={setVars} keyPlaceholder="base_url" valuePlaceholder="$.deployment.url" addLabel="Add variable" monoValue />
            </Field>
            <Field label="Labels from payload" optional hint="Shown on runs and usable as filters.">
              <KvEditor value={labels} onChange={setLabels} keyPlaceholder="git_sha" valuePlaceholder="$.after" addLabel="Add label" monoValue />
            </Field>
          </>
        )}
      </div>
    </Dialog>
  )
}

export function TriggersPanel({ suiteId }: { suiteId: string }) {
  const q = useTriggers(suiteId)
  const { update, remove, rotate } = useWebhookMutations(suiteId)
  const [adding, setAdding] = useState(false)
  const [reveal, setReveal] = useState<S['TriggerCreated'] | null>(null)
  const [toDelete, setToDelete] = useState<S['Trigger'] | null>(null)
  const items = (q.data?.items ?? []).filter((t) => t.kind !== 'manual')

  return (
    <div className="flex flex-col gap-3">
      <Card>
        <CardHeader title="Triggers" description="What starts a run besides the Run button." actions={<Button size="sm" icon={<Plus />} onClick={() => setAdding(true)}>Add trigger</Button>} />
        {q.isLoading ? (
          <CardBody><Skeleton className="h-20 w-full" /></CardBody>
        ) : q.error ? (
          <ErrorPanel error={q.error} onRetry={() => void q.refetch()} />
        ) : items.length === 0 ? (
          <EmptyState compact illustration="suites" title="No triggers yet" description="Run on a schedule, or from a webhook such as a deploy event." action={<Button size="sm" variant="primary" icon={<Plus />} onClick={() => setAdding(true)}>Add trigger</Button>} />
        ) : (
          <ul className="divide-y divide-border">
            {items.map((t) => (
              <li key={t.id} className="flex flex-col gap-3 px-4 py-3">
                <div className="flex items-center gap-3">
                  <span className="flex size-8 items-center justify-center rounded-md border border-border bg-surface-2 text-text-2 [&_svg]:size-4">{t.kind === 'schedule' ? <Calendar /> : <Webhook />}</span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium text-text">{t.name}</span>
                      <Badge>{t.kind}</Badge>
                    </div>
                    <div className="text-xs text-text-muted">
                      {t.kind === 'schedule' ? (
                        <>
                          <code className="font-mono">{String((t.config as Record<string, unknown>).cron ?? '')}</code> · next <RelTime iso={t.next_fire_at} className="text-xs text-text-muted" />
                        </>
                      ) : (
                        <>Last fired <RelTime iso={t.last_fired_at} className="text-xs text-text-muted" /></>
                      )}
                    </div>
                  </div>
                  <Switch size="sm" aria-label={`${t.name} enabled`} checked={t.enabled} onCheckedChange={(v) => update.mutate({ id: t.id, body: { enabled: v } })} />
                  <IconButton label={`Delete ${t.name}`} size="sm" onClick={() => setToDelete(t)}><Trash2 /></IconButton>
                </div>
                {t.kind === 'webhook' && t.webhook_url && (
                  <div className="flex flex-col gap-2 pl-11">
                    <div className="flex items-center gap-2 rounded-sm border border-border bg-surface-2 px-2.5 py-1.5">
                      <code className="min-w-0 flex-1 truncate font-mono text-xs text-text">{absolute(t.webhook_url)}</code>
                      <CopyButton value={absolute(t.webhook_url)} label="Copy URL" />
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <CopyButton value={webhookCurl(t.webhook_url)} label="Copy sample cURL" />
                      <span className="text-xs text-text-muted">Sample cURL</span>
                      <Button size="sm" variant="ghost" icon={<RefreshCw />} loading={rotate.isPending} onClick={() => rotate.mutate(t.id, { onSuccess: setReveal, onError: (e) => toast.error(errorMessage(e)) })}>
                        Rotate secret
                      </Button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Card>
        <CardHeader title="API / CI" description="Start runs from your pipeline with an API key (Settings → API keys)." />
        <CardBody className="flex flex-col gap-3">
          <CodeBlock language="bash" maxHeight={160} code={`curl -X POST ${window.location.origin}/v1/runs \\\n  -H "Authorization: Bearer $AIRVAL_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"suite_id": "${suiteId}", "labels": {"git_sha": "'$GITHUB_SHA'"}}'`} />
          <p className="flex items-center gap-1.5 text-xs text-text-muted"><KeyRound className="size-3.5" /> Or use the CLI: <code className="font-mono text-text-2">airval run --suite {suiteId.slice(0, 8)}…</code></p>
        </CardBody>
      </Card>
      <AddDialog suiteId={suiteId} open={adding} onOpenChange={setAdding} onCreated={setReveal} />
      <SecretReveal trigger={reveal} onClose={() => setReveal(null)} />
      <ConfirmDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)} title={`Delete ${toDelete?.name ?? 'trigger'}?`} description="It stops firing immediately." loading={remove.isPending} onConfirm={() => toDelete && remove.mutate(toDelete.id, { onSuccess: () => setToDelete(null), onError: (e) => toast.error(errorMessage(e)) })} />
    </div>
  )
}

