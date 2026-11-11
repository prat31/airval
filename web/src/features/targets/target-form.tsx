import { useMemo, useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/cn'
import type { Obj } from '../shared/api'
import { getIn, setIn } from '../shared/obj'
import { useSecrets } from '../shared/secrets'
import { FieldRow, FormSection, JsonEditor, KvEditor, TemplateField, type TemplateHint } from '../shared/ui'
import { isHttpLike, isLLM, type TargetKind } from './kinds'

interface Props {
  kind: TargetKind
  config: Obj
  onChange: (c: Obj) => void
  errors: Record<string, string>
  /** Example input fields offered as template hints (e.g. from the project's datasets). */
  inputFields?: string[]
}

const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((m) => ({ value: m, label: m }))

/** Collapsible group for rarely-changed options. */
function Advanced({ title, children }: { title: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)
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

function Toggle({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <div className="text-sm font-medium text-text">{label}</div>
        {hint && <p className="mt-0.5 text-xs text-text-muted">{hint}</p>}
      </div>
      <Switch aria-label={label} checked={checked} onCheckedChange={onChange} />
    </div>
  )
}

export function TargetForm({ kind, config, onChange, errors, inputFields = ['question'] }: Props) {
  const { data: secrets = [] } = useSecrets()
  const get = <T,>(path: string, fallback?: T) => getIn<T>(config, path.split('.'), fallback)
  const set = (path: string, v: unknown) => onChange(setIn(config, path.split('.'), v))
  const numVal = (path: string) => (get<number | undefined>(path) === undefined ? '' : String(get<number>(path)))
  const setNum = (path: string) => (e: React.ChangeEvent<HTMLInputElement>) => set(path, e.target.value === '' ? undefined : Number(e.target.value))

  const inputHints = useMemo<TemplateHint[]>(() => inputFields.map((f) => ({ label: `.input.${f}`, insert: `{{ .input.${f} }}`, title: 'Example input field' })), [inputFields])
  const secretHints = useMemo<TemplateHint[]>(() => secrets.slice(0, 5).map((s) => ({ label: `secret ${s.name}`, insert: `{{ secret "${s.name}" }}`, title: 'Secret reference (resolved server-side)' })), [secrets])
  const allHints = [...inputHints, ...secretHints]

  const text = (label: string, path: string, opts: { placeholder?: string; mono?: boolean; optional?: boolean; hint?: string; secret?: boolean } = {}) => (
    <Field label={label} hint={opts.hint} error={errors[path]} optional={opts.optional}>
      {opts.secret ? (
        <TemplateField value={get<string>(path, '')} onChange={(v) => set(path, v)} hints={secretHints} placeholder={opts.placeholder ?? '{{ secret "NAME" }}'} />
      ) : (
        <Input className={opts.mono ? 'font-mono text-xs' : undefined} value={get<string>(path, '')} placeholder={opts.placeholder} onChange={(e) => set(path, e.target.value)} />
      )}
    </Field>
  )
  const path = (label: string, p: string, placeholder: string, hint?: string) => text(label, p, { placeholder, mono: true, optional: true, hint })

  return (
    <div className="flex flex-col">
      {isHttpLike(kind) && (
        <>
          <FormSection title="Request" description={'Strings are templates: {{ .input.question }} and {{ secret "KEY" }}.'}>
            <div className="grid grid-cols-[110px_1fr] gap-3">
              <Field label="Method">
                <Select value={get<string>('method', 'POST')} onValueChange={(v) => set('method', v)} options={methods} />
              </Field>
              <Field label="URL" error={errors.url}>
                <TemplateField value={get<string>('url', '')} onChange={(v) => set('url', v)} placeholder="https://api.example.com/v1/answer" hints={secretHints} />
              </Field>
            </div>
            <Field label="Headers" hint={'Reference credentials with {{ secret "NAME" }}; values are never logged.'}>
              <KvEditor value={get('headers')} onChange={(v) => set('headers', v)} keyPlaceholder="Header" valuePlaceholder="Value" addLabel="Add header" monoValue />
            </Field>
            {get<string>('method', 'POST') !== 'GET' && (
              <Field label="Body" optional>
                <TemplateField rows={8} value={get<string>('body', '')} onChange={(v) => set('body', v)} hints={allHints} placeholder={'{ "question": {{ json .input.question }} }'} />
              </Field>
            )}
          </FormSection>

          <FormSection title="Response mapping" description="JSONPath expressions evaluated on the response body. The playground shows what each one matches.">
            <Field label="Output" hint="Where the answer text lives. Empty uses the whole body." error={errors['response.output']} optional>
              <Input className="font-mono text-xs" value={get<string>('response.output', '')} placeholder="$.choices[0].message.content" onChange={(e) => set('response.output', e.target.value)} />
            </Field>
            <FieldRow>
              {path('Context', 'response.context', '$.sources[*].text', 'Retrieved passages.')}
              {path('Output JSON', 'response.output_json', '$.result', 'Structured output for JSON evaluators.')}
            </FieldRow>
            <FieldRow>
              {path(kind === 'agent' ? 'Trace steps (required)' : 'Trace steps', 'response.trace.path', '$.steps', 'Array of LLM / tool / retrieval steps.')}
              {path('Tool calls', 'response.tool_calls.path', '$.tool_calls')}
            </FieldRow>
            <FieldRow cols={3}>
              {path('Input tokens', 'response.input_tokens', '$.usage.prompt_tokens')}
              {path('Output tokens', 'response.output_tokens', '$.usage.completion_tokens')}
              {path('Cost (USD)', 'response.cost_usd', '$.cost')}
            </FieldRow>
          </FormSection>

          <FormSection title="Streaming, sessions & limits">
            <Toggle label="Streaming response" hint="Records time to first token." checked={!!get('stream')} onChange={(v) => set('stream', v ? { mode: 'sse', delta_path: '$.delta' } : undefined)} />
            {!!get('stream') && (
              <FieldRow>
                <Field label="Format">
                  <Select value={get<string>('stream.mode', 'sse')} onValueChange={(v) => set('stream.mode', v)} options={[{ value: 'sse', label: 'Server-sent events' }, { value: 'ndjson', label: 'NDJSON' }]} />
                </Field>
                <Field label="Delta path" hint="Text delta inside each chunk.">
                  <Input className="font-mono text-xs" value={get<string>('stream.delta_path', '')} placeholder="$.choices[0].delta.content" onChange={(e) => set('stream.delta_path', e.target.value)} />
                </Field>
              </FieldRow>
            )}
            {path('Session ID path', 'session.path', '$.session_id', 'Multi-turn: extracted from the response and fed back as the session on the next turn.')}
            <FieldRow>
              <Field label="Timeout (ms)">
                <Input type="number" min={1} value={numVal('timeout_ms')} onChange={setNum('timeout_ms')} />
              </Field>
              <Field label="Max concurrency" optional>
                <Input type="number" min={1} value={numVal('max_concurrency')} placeholder="auto" onChange={setNum('max_concurrency')} />
              </Field>
            </FieldRow>
            <Advanced title="TLS & client certificates">
              <Toggle label="Skip certificate verification" hint="Only for self-signed staging endpoints." checked={get<boolean>('tls.insecure_skip_verify', false)} onChange={(v) => set('tls.insecure_skip_verify', v || undefined)} />
              <FieldRow>
                {text('Server name', 'tls.server_name', { optional: true })}
                {text('CA certificate', 'tls.ca_cert', { optional: true, secret: true })}
              </FieldRow>
              <FieldRow>
                {text('Client certificate', 'tls.client_cert', { optional: true, secret: true })}
                {text('Client key', 'tls.client_key', { optional: true, secret: true })}
              </FieldRow>
            </Advanced>
          </FormSection>
        </>
      )}

      {isLLM(kind) && (
        <>
          <FormSection title="Connection" description="Credentials are secret references, e.g. {{ secret &quot;OPENAI_KEY&quot; }}. Values are encrypted at rest and never shown again.">
            {kind === 'openai' && (
              <>
                {text('Base URL', 'base_url', { mono: true, hint: 'Any OpenAI-compatible endpoint: Ollama, vLLM, Together, Groq, Azure…' })}
                {text('API key', 'api_key', { secret: true, optional: true })}
                <Advanced title="Azure, paths & headers">
                  <FieldRow>
                    {text('Path', 'path', { mono: true, optional: true, placeholder: '/chat/completions' })}
                    {text('Auth header', 'auth_header', { optional: true, placeholder: 'Authorization (use api-key for Azure)' })}
                  </FieldRow>
                  {text('Max tokens parameter', 'max_tokens_param', { optional: true, placeholder: 'max_tokens (max_completion_tokens for o-series)' })}
                  <Field label="Query parameters" optional hint="e.g. api-version for Azure">
                    <KvEditor value={get('query')} onChange={(v) => set('query', v)} addLabel="Add parameter" monoValue />
                  </Field>
                  <Field label="Headers" optional>
                    <KvEditor value={get('headers')} onChange={(v) => set('headers', v)} addLabel="Add header" monoValue />
                  </Field>
                </Advanced>
              </>
            )}
            {kind === 'anthropic' && (
              <>
                {text('Base URL', 'base_url', { mono: true })}
                {text('API key', 'api_key', { secret: true, optional: true })}
                <FieldRow>
                  {text('API version', 'anthropic_version', { optional: true, placeholder: '2023-06-01' })}
                  {text('Beta features', 'anthropic_beta', { optional: true })}
                </FieldRow>
              </>
            )}
            {kind === 'bedrock' && (
              <>
                <FieldRow>
                  {text('Region', 'region', { placeholder: 'us-east-1' })}
                  {text('Endpoint', 'endpoint', { mono: true, optional: true })}
                </FieldRow>
                <FieldRow cols={3}>
                  {text('Access key ID', 'access_key_id', { secret: true })}
                  {text('Secret access key', 'secret_access_key', { secret: true })}
                  {text('Session token', 'session_token', { secret: true, optional: true })}
                </FieldRow>
              </>
            )}
            {kind === 'vertex' && (
              <>
                <FieldRow>
                  {text('GCP project', 'project_id', { optional: true, hint: 'Vertex AI. Leave empty for the Gemini API.' })}
                  {text('Location', 'location', { optional: true, placeholder: 'us-central1' })}
                </FieldRow>
                <FieldRow>
                  {text('API key', 'api_key', { secret: true, optional: true })}
                  {text('Access token', 'access_token', { secret: true, optional: true })}
                </FieldRow>
                {text('Base URL', 'base_url', { mono: true, optional: true })}
              </>
            )}
          </FormSection>

          <FormSection title="Model & prompt">
            {text('Model', 'model', { mono: true, hint: kind === 'bedrock' ? 'Bedrock model ID, e.g. anthropic.claude-3-5-sonnet-20241022-v2:0' : undefined })}
            <Field label="System prompt" optional>
              <TemplateField rows={4} value={get<string>('system_prompt', '')} onChange={(v) => set('system_prompt', v)} hints={allHints} placeholder="You are a helpful support assistant…" />
            </Field>
            <Field label="User message template" optional hint="Defaults to the example's input.">
              <TemplateField rows={3} value={get<string>('user_template', '')} onChange={(v) => set('user_template', v)} hints={inputHints} placeholder="{{ .input.question }}" />
            </Field>
            <FieldRow cols={3}>
              <Field label="Temperature" optional>
                <Input type="number" min={0} max={2} step={0.1} value={numVal('temperature')} onChange={setNum('temperature')} />
              </Field>
              <Field label="Max tokens" optional>
                <Input type="number" min={1} value={numVal('max_tokens')} onChange={setNum('max_tokens')} />
              </Field>
              <Field label="Timeout (ms)" optional>
                <Input type="number" min={1} value={numVal('timeout_ms')} onChange={setNum('timeout_ms')} />
              </Field>
            </FieldRow>
            {(kind === 'openai' || kind === 'anthropic') && <Toggle label="Stream responses" hint="Records time to first token." checked={get<boolean>('stream', false)} onChange={(v) => set('stream', v || undefined)} />}
            <Advanced title="Tools, extra body & pricing">
              {kind !== 'bedrock' && (
                <Field label="Tools" hint="Provider-native tool definitions (JSON array)." optional>
                  <JsonEditor value={get('tools')} onChange={(v) => set('tools', v)} rows={5} placeholder="[ ]" />
                </Field>
              )}
              {kind === 'bedrock' && (
                <Field label="Tool config" optional>
                  <JsonEditor value={get('tool_config')} onChange={(v) => set('tool_config', v)} rows={4} placeholder="{ }" />
                </Field>
              )}
              {kind === 'openai' && (
                <Field label="Response format" optional hint='e.g. {"type": "json_object"}'>
                  <JsonEditor value={get('response_format')} onChange={(v) => set('response_format', v)} rows={3} placeholder="{ }" />
                </Field>
              )}
              <Field label="Extra body" hint="Merged into the provider request." optional>
                <JsonEditor value={get('extra_body')} onChange={(v) => set('extra_body', v)} rows={4} placeholder="{ }" />
              </Field>
              <FieldRow>
                <Field label="Input price / 1M tokens (USD)" optional>
                  <Input type="number" min={0} step="any" value={numVal('pricing.input_per_1m')} onChange={setNum('pricing.input_per_1m')} />
                </Field>
                <Field label="Output price / 1M tokens (USD)" optional>
                  <Input type="number" min={0} step="any" value={numVal('pricing.output_per_1m')} onChange={setNum('pricing.output_per_1m')} />
                </Field>
              </FieldRow>
            </Advanced>
          </FormSection>
        </>
      )}
    </div>
  )
}
