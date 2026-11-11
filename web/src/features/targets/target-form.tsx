import { useMemo, useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/cn'
import type { Obj } from '../shared/api'
import { getIn, setIn } from '../shared/obj'
import { SecretPicker, useSecrets } from '../shared/secrets'
import { FieldRow, FormSection, JsonEditor, KvEditor, TemplateField, type TemplateHint } from '../shared/ui'
import { isHttpLike, isLLM, sectionOf, type TargetKind } from './kinds'

interface Props {
  kind: TargetKind
  config: Obj
  onChange: (c: Obj) => void
  errors: Record<string, string>
  /** Example input fields offered as template hints (e.g. from the project's datasets). */
  inputFields?: string[]
}

const methods = ['GET', 'POST', 'PUT', 'PATCH'].map((m) => ({ value: m, label: m }))

/** Collapsible group for rarely-changed options. */
function Advanced({ title, children, defaultOpen }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(!!defaultOpen)
  return (
    <div className="rounded-md border border-border">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="flex w-full items-center gap-1.5 px-3 py-2 text-left text-sm font-medium text-text-2 hover:text-text focus-ring">
        <ChevronRight className={cn('size-3.5 transition-transform duration-150', open && 'rotate-90')} />
        {title}
      </button>
      {open && <div className="flex flex-col gap-4 border-t border-border p-3">{children}</div>}
    </div>
  )
}

export function TargetForm({ kind, config, onChange, errors, inputFields = ['question'] }: Props) {
  const sec = sectionOf(kind)
  const { data: secrets = [] } = useSecrets()
  const get = <T,>(path: string[], fallback?: T) => getIn<T>(config, path, fallback)
  const set = (path: string[], v: unknown) => onChange(setIn(config, path, v))
  const err = (path: string[]) => errors[path.join('.')]
  const vars = Object.keys(get<Record<string, string>>(['variables'], {}))

  const hints = useMemo<TemplateHint[]>(
    () => [
      ...inputFields.map((f) => ({ label: `.input.${f}`, insert: `{{ .input.${f} }}`, title: 'Example input field' })),
      ...vars.map((v) => ({ label: `.vars.${v}`, insert: `{{ .vars.${v} }}`, title: 'Target variable' })),
      ...secrets.slice(0, 4).map((s) => ({ label: `secret ${s.name}`, insert: `{{ secret "${s.name}" }}`, title: 'Secret reference (resolved server-side)' })),
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [inputFields.join(','), vars.join(','), secrets],
  )
  const secretField = (label: string, key: string, hint?: string) => (
    <Field label={label} hint={hint} error={err([sec, key])} optional>
      <SecretPicker value={get<string>([sec, key], '')} onChange={(v) => set([sec, key], v)} />
    </Field>
  )

  return (
    <div className="flex flex-col">
      {isHttpLike(kind) && (
        <>
          <FormSection title="Request" description="Strings are templates: {{ .input.question }}, {{ .vars.base_url }}, {{ secret &quot;KEY&quot; }}.">
            <div className="grid grid-cols-[110px_1fr] gap-3">
              <Field label="Method">
                <Select value={get<string>(['http', 'method'], 'POST')} onValueChange={(v) => set(['http', 'method'], v)} options={methods} />
              </Field>
              <Field label="URL" error={err(['http', 'url'])}>
                <TemplateField value={get<string>(['http', 'url'], '')} onChange={(v) => set(['http', 'url'], v)} placeholder="https://api.example.com/v1/answer" hints={hints.filter((h) => !h.insert.includes('.input.'))} />
              </Field>
            </div>
            <Field label="Headers" hint="Reference credentials with {{ secret &quot;NAME&quot; }}; values are never logged.">
              <KvEditor value={get(['http', 'headers'])} onChange={(v) => set(['http', 'headers'], v)} keyPlaceholder="Header" valuePlaceholder="Value" addLabel="Add header" monoValue />
            </Field>
            <Field label="Query parameters" optional>
              <KvEditor value={get(['http', 'query'])} onChange={(v) => set(['http', 'query'], v)} keyPlaceholder="Name" valuePlaceholder="Value" addLabel="Add parameter" monoValue />
            </Field>
            {get<string>(['http', 'method'], 'POST') !== 'GET' && (
              <Field label="Body" optional>
                <TemplateField rows={8} value={get<string>(['http', 'body'], '')} onChange={(v) => set(['http', 'body'], v)} hints={hints} placeholder={'{ "question": "{{ .input.question }}" }'} />
              </Field>
            )}
          </FormSection>

          <FormSection title="Response mapping" description="JSONPath expressions evaluated on the response body. Use the playground to see what each one matches.">
            <Field label="Output" hint="Where the answer text lives." error={err(['http', 'response', 'output'])}>
              <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'output'], '')} placeholder="$.choices[0].message.content" onChange={(e) => set(['http', 'response', 'output'], e.target.value)} />
            </Field>
            <FieldRow>
              <Field label="Context" hint="Array of retrieved passages." optional error={err(['http', 'response', 'context'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'context'], '')} placeholder="$.sources[*].text" onChange={(e) => set(['http', 'response', 'context'], e.target.value)} />
              </Field>
              <Field label="Tool calls" optional error={err(['http', 'response', 'tool_calls'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'tool_calls'], '')} placeholder="$.tool_calls" onChange={(e) => set(['http', 'response', 'tool_calls'], e.target.value)} />
              </Field>
            </FieldRow>
            {kind === 'agent' && (
              <Field label="Trace" hint="Intermediate steps: LLM calls, tool calls and retrievals." optional error={err(['http', 'response', 'trace'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'trace'], '')} placeholder="$.steps" onChange={(e) => set(['http', 'response', 'trace'], e.target.value)} />
              </Field>
            )}
            <FieldRow cols={3}>
              <Field label="Input tokens" optional error={err(['http', 'response', 'usage_input_tokens'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'usage_input_tokens'], '')} placeholder="$.usage.prompt_tokens" onChange={(e) => set(['http', 'response', 'usage_input_tokens'], e.target.value)} />
              </Field>
              <Field label="Output tokens" optional error={err(['http', 'response', 'usage_output_tokens'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'usage_output_tokens'], '')} placeholder="$.usage.completion_tokens" onChange={(e) => set(['http', 'response', 'usage_output_tokens'], e.target.value)} />
              </Field>
              <Field label="Cost (USD)" optional error={err(['http', 'response', 'cost_usd'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'cost_usd'], '')} placeholder="$.cost" onChange={(e) => set(['http', 'response', 'cost_usd'], e.target.value)} />
              </Field>
            </FieldRow>
          </FormSection>

          <FormSection title="Streaming & reliability">
            <FieldRow cols={3}>
              <Field label="Streaming">
                <Select
                  value={get<string>(['http', 'streaming'], 'none')}
                  onValueChange={(v) => set(['http', 'streaming'], v === 'none' ? undefined : v)}
                  options={[
                    { value: 'none', label: 'Off' },
                    { value: 'sse', label: 'Server-sent events' },
                    { value: 'ndjson', label: 'NDJSON' },
                  ]}
                />
              </Field>
              <Field label="Timeout (ms)">
                <Input type="number" min={1} value={String(get<number>(['http', 'timeout_ms'], 60000))} onChange={(e) => set(['http', 'timeout_ms'], e.target.value === '' ? undefined : Number(e.target.value))} />
              </Field>
              <Field label="Retries">
                <Input type="number" min={0} max={10} value={String(get<number>(['http', 'retries'], 2))} onChange={(e) => set(['http', 'retries'], e.target.value === '' ? undefined : Number(e.target.value))} />
              </Field>
            </FieldRow>
            {get<string>(['http', 'streaming'], 'none') !== 'none' && (
              <Field label="Stream delta path" hint="Path of the text delta inside each chunk." error={err(['http', 'response', 'stream_delta'])}>
                <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'stream_delta'], '')} placeholder="$.choices[0].delta.content" onChange={(e) => set(['http', 'response', 'stream_delta'], e.target.value)} />
              </Field>
            )}
            <Advanced title="TLS & client certificates">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="text-sm font-medium text-text">Skip certificate verification</div>
                  <p className="mt-0.5 text-xs text-text-muted">Only for self-signed staging endpoints.</p>
                </div>
                <Switch aria-label="Skip certificate verification" checked={get<boolean>(['http', 'tls', 'insecure_skip_verify'], false)} onCheckedChange={(v) => set(['http', 'tls', 'insecure_skip_verify'], v || undefined)} />
              </div>
              <FieldRow cols={3}>
                <Field label="CA certificate" optional>
                  <SecretPicker value={get<string>(['http', 'tls', 'ca_cert_secret'], '')} onChange={(v) => set(['http', 'tls', 'ca_cert_secret'], v)} />
                </Field>
                <Field label="Client certificate" optional>
                  <SecretPicker value={get<string>(['http', 'tls', 'client_cert_secret'], '')} onChange={(v) => set(['http', 'tls', 'client_cert_secret'], v)} />
                </Field>
                <Field label="Client key" optional>
                  <SecretPicker value={get<string>(['http', 'tls', 'client_key_secret'], '')} onChange={(v) => set(['http', 'tls', 'client_key_secret'], v)} />
                </Field>
              </FieldRow>
            </Advanced>
          </FormSection>
        </>
      )}

      {isLLM(kind) && (
        <>
          <FormSection title="Connection" description="Credentials are secret references. Values are encrypted at rest and never shown again.">
            {kind === 'openai' && (
              <>
                <Field label="Base URL" hint="Any OpenAI-compatible endpoint: Ollama, vLLM, Together, Groq, Azure…" error={err(['openai', 'base_url'])}>
                  <Input className="font-mono text-xs" value={get<string>(['openai', 'base_url'], '')} onChange={(e) => set(['openai', 'base_url'], e.target.value)} />
                </Field>
                {secretField('API key', 'api_key_secret')}
                <Advanced title="Azure OpenAI & organization">
                  <FieldRow>
                    <Field label="Deployment" optional>
                      <Input value={get<string>(['openai', 'deployment'], '')} onChange={(e) => set(['openai', 'deployment'], e.target.value)} />
                    </Field>
                    <Field label="API version" optional>
                      <Input value={get<string>(['openai', 'api_version'], '')} placeholder="2024-10-21" onChange={(e) => set(['openai', 'api_version'], e.target.value)} />
                    </Field>
                  </FieldRow>
                  <Field label="Organization" optional>
                    <Input value={get<string>(['openai', 'organization'], '')} onChange={(e) => set(['openai', 'organization'], e.target.value)} />
                  </Field>
                </Advanced>
              </>
            )}
            {kind === 'anthropic' && (
              <>
                <Field label="Base URL" optional>
                  <Input className="font-mono text-xs" value={get<string>(['anthropic', 'base_url'], '')} placeholder="https://api.anthropic.com" onChange={(e) => set(['anthropic', 'base_url'], e.target.value)} />
                </Field>
                {secretField('API key', 'api_key_secret')}
              </>
            )}
            {kind === 'bedrock' && (
              <>
                <FieldRow>
                  <Field label="Region" error={err(['bedrock', 'region'])}>
                    <Input value={get<string>(['bedrock', 'region'], '')} placeholder="us-east-1" onChange={(e) => set(['bedrock', 'region'], e.target.value)} />
                  </Field>
                  <Field label="Model ID" error={err(['bedrock', 'model_id'])}>
                    <Input className="font-mono text-xs" value={get<string>(['bedrock', 'model_id'], '')} placeholder="anthropic.claude-3-5-sonnet-20241022-v2:0" onChange={(e) => set(['bedrock', 'model_id'], e.target.value)} />
                  </Field>
                </FieldRow>
                <FieldRow cols={3}>
                  {secretField('Access key ID', 'access_key_id_secret')}
                  {secretField('Secret access key', 'secret_access_key_secret')}
                  {secretField('Session token', 'session_token_secret')}
                </FieldRow>
              </>
            )}
            {kind === 'vertex' && (
              <>
                <FieldRow>
                  <Field label="GCP project" error={err(['vertex', 'project'])}>
                    <Input value={get<string>(['vertex', 'project'], '')} onChange={(e) => set(['vertex', 'project'], e.target.value)} />
                  </Field>
                  <Field label="Location" error={err(['vertex', 'location'])}>
                    <Input value={get<string>(['vertex', 'location'], '')} placeholder="us-central1" onChange={(e) => set(['vertex', 'location'], e.target.value)} />
                  </Field>
                </FieldRow>
                {secretField('Service account JSON', 'credentials_secret', 'A secret containing the service account key.')}
              </>
            )}
          </FormSection>

          <FormSection title="Model & prompt">
            {kind !== 'bedrock' && (
              <Field label="Model" error={err([sec, 'model'])}>
                <Input className="font-mono text-xs" value={get<string>([sec, 'model'], '')} onChange={(e) => set([sec, 'model'], e.target.value)} />
              </Field>
            )}
            <Field label="System prompt" optional>
              <TemplateField rows={4} value={get<string>([sec, 'system_prompt'], '')} onChange={(v) => set([sec, 'system_prompt'], v)} hints={hints} placeholder="You are a helpful support assistant…" />
            </Field>
            <Field label="User message template" hint="Defaults to the example's input.">
              <TemplateField rows={3} value={get<string>([sec, 'prompt_template'], '')} onChange={(v) => set([sec, 'prompt_template'], v)} hints={hints} placeholder="{{ .input.question }}" />
            </Field>
            <FieldRow cols={3}>
              <Field label="Temperature" optional>
                <Input type="number" min={0} max={2} step={0.1} value={get<number | undefined>([sec, 'temperature']) === undefined ? '' : String(get<number>([sec, 'temperature']))} onChange={(e) => set([sec, 'temperature'], e.target.value === '' ? undefined : Number(e.target.value))} />
              </Field>
              <Field label="Max tokens" optional>
                <Input type="number" min={1} value={get<number | undefined>([sec, 'max_tokens']) === undefined ? '' : String(get<number>([sec, 'max_tokens']))} onChange={(e) => set([sec, 'max_tokens'], e.target.value === '' ? undefined : Number(e.target.value))} />
              </Field>
              <Field label="Top P" optional>
                <Input type="number" min={0} max={1} step={0.05} value={get<number | undefined>([sec, 'top_p']) === undefined ? '' : String(get<number>([sec, 'top_p']))} onChange={(e) => set([sec, 'top_p'], e.target.value === '' ? undefined : Number(e.target.value))} />
              </Field>
            </FieldRow>
            <div className="flex items-start justify-between gap-4">
              <div>
                <div className="text-sm font-medium text-text">JSON mode</div>
                <p className="mt-0.5 text-xs text-text-muted">Ask the model to return a JSON object.</p>
              </div>
              <Switch aria-label="JSON mode" checked={get<boolean>([sec, 'json_mode'], false)} onCheckedChange={(v) => set([sec, 'json_mode'], v || undefined)} />
            </div>
            <Advanced title="Tools & extra request body">
              <Field label="Tools" hint="Provider-native tool definitions (JSON array)." optional>
                <JsonEditor value={get([sec, 'tools'])} onChange={(v) => set([sec, 'tools'], v)} rows={6} placeholder="[ ]" />
              </Field>
              <Field label="Extra body" hint="Merged into the provider request." optional>
                <JsonEditor value={get([sec, 'extra_body'])} onChange={(v) => set([sec, 'extra_body'], v)} rows={4} placeholder="{ }" />
              </Field>
            </Advanced>
          </FormSection>
        </>
      )}

      <FormSection title="Multi-turn" description="How conversation state is carried between scripted or simulated turns.">
        <FieldRow>
          <Field label="State">
            <Select
              value={get<string>(['conversation', 'state'], 'messages')}
              onValueChange={(v) => set(['conversation', 'state'], v === 'messages' ? undefined : v)}
              options={[
                { value: 'messages', label: 'Messages array' },
                { value: 'session_id', label: 'Session ID' },
              ]}
            />
          </Field>
          {get<string>(['conversation', 'state'], 'messages') === 'messages' ? (
            <Field label="Messages variable" hint="Template variable that holds the history.">
              <Input className="font-mono text-xs" value={get<string>(['conversation', 'messages_variable'], '')} placeholder="messages" onChange={(e) => set(['conversation', 'messages_variable'], e.target.value)} />
            </Field>
          ) : (
            <Field label="Session ID path" hint="Mapped from the response and sent back next turn." error={err(['http', 'response', 'session_id'])}>
              <Input className="font-mono text-xs" value={get<string>(['http', 'response', 'session_id'], '')} placeholder="$.session_id" onChange={(e) => set(['http', 'response', 'session_id'], e.target.value)} />
            </Field>
          )}
        </FieldRow>
      </FormSection>

      <FormSection title="Limits & caching" description="Applied across all runs against this target. Adaptive limiting backs off automatically on 429 / 5xx.">
        <FieldRow cols={3}>
          <Field label="Max concurrency" optional>
            <Input type="number" min={1} value={get<number | undefined>(['limits', 'max_concurrency']) === undefined ? '' : String(get<number>(['limits', 'max_concurrency']))} placeholder="auto" onChange={(e) => set(['limits', 'max_concurrency'], e.target.value === '' ? undefined : Number(e.target.value))} />
          </Field>
          <Field label="Requests / min" optional>
            <Input type="number" min={0} value={get<number | undefined>(['limits', 'rpm']) === undefined ? '' : String(get<number>(['limits', 'rpm']))} placeholder="unlimited" onChange={(e) => set(['limits', 'rpm'], e.target.value === '' ? undefined : Number(e.target.value))} />
          </Field>
          <Field label="Tokens / min" optional>
            <Input type="number" min={0} value={get<number | undefined>(['limits', 'tpm']) === undefined ? '' : String(get<number>(['limits', 'tpm']))} placeholder="unlimited" onChange={(e) => set(['limits', 'tpm'], e.target.value === '' ? undefined : Number(e.target.value))} />
          </Field>
        </FieldRow>
        <div className="flex items-start justify-between gap-4">
          <div>
            <div className="text-sm font-medium text-text">Adaptive rate limiting</div>
            <p className="mt-0.5 text-xs text-text-muted">Raise throughput until the service pushes back, then ease off.</p>
          </div>
          <Switch aria-label="Adaptive rate limiting" checked={get<boolean>(['limits', 'adaptive'], true)} onCheckedChange={(v) => set(['limits', 'adaptive'], v ? undefined : false)} />
        </div>
        <div className="flex items-start justify-between gap-4">
          <div>
            <div className="text-sm font-medium text-text">Cache responses</div>
            <p className="mt-0.5 text-xs text-text-muted">Reuse identical requests across runs. Great for judge-only reruns.</p>
          </div>
          <Switch aria-label="Cache responses" checked={get<boolean>(['cache', 'enabled'], false)} onCheckedChange={(v) => set(['cache', 'enabled'], v || undefined)} />
        </div>
        {get<boolean>(['cache', 'enabled'], false) && (
          <Field label="Cache TTL (seconds)">
            <Input type="number" min={1} value={String(get<number>(['cache', 'ttl_seconds'], 86400))} onChange={(e) => set(['cache', 'ttl_seconds'], e.target.value === '' ? undefined : Number(e.target.value))} />
          </Field>
        )}
      </FormSection>

      <FormSection title="Variables" description="Defaults for {{ .vars.name }}. Overridable per run and by webhook payloads (e.g. a deploy preview URL).">
        <KvEditor value={get(['variables'])} onChange={(v) => set(['variables'], v)} keyPlaceholder="name" valuePlaceholder="default value" addLabel="Add variable" monoValue />
      </FormSection>
    </div>
  )
}
