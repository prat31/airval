import { Bot, Cloud, Globe, Network, Sparkles, Waypoints, type LucideIcon } from 'lucide-react'
import type { Obj, S } from '../shared/api'
import { parsePath } from '../shared/jsonpath'
import { getIn, isObj } from '../shared/obj'

export type TargetKind = S['TargetKind']

export interface KindMeta {
  label: string
  description: string
  icon: LucideIcon
}

export const kindOrder: TargetKind[] = ['http', 'openai', 'anthropic', 'bedrock', 'vertex', 'agent']

export const kindMeta: Record<TargetKind, KindMeta> = {
  http: { label: 'HTTP endpoint', description: 'Any REST or streaming API. Map the response with JSONPath.', icon: Globe },
  openai: { label: 'OpenAI-compatible', description: 'OpenAI, Azure OpenAI, Ollama, vLLM, Together, Groq…', icon: Sparkles },
  anthropic: { label: 'Anthropic', description: 'Claude via the Messages API, with tools and system prompt.', icon: Bot },
  bedrock: { label: 'AWS Bedrock', description: 'Foundation models on Bedrock with access-key secrets.', icon: Cloud },
  vertex: { label: 'Google Vertex AI', description: 'Gemini and partner models via Vertex or the Gemini API.', icon: Network },
  agent: { label: 'Agent (HTTP)', description: 'An HTTP agent that also returns a trace: steps, tool calls, retrieved docs.', icon: Waypoints },
}

export const isLLM = (k: TargetKind) => k === 'openai' || k === 'anthropic' || k === 'bedrock' || k === 'vertex'
export const isHttpLike = (k: TargetKind) => k === 'http' || k === 'agent'

/** Target configs are flat and specific to the kind (the server rejects unknown fields). */
export function defaultConfig(kind: TargetKind): Obj {
  switch (kind) {
    case 'http':
      return {
        method: 'POST',
        url: '',
        headers: { 'Content-Type': 'application/json' },
        body: '{\n  "question": {{ json .input.question }}\n}',
        response: { output: '$.answer' },
        timeout_ms: 60000,
      }
    case 'agent':
      return {
        method: 'POST',
        url: '',
        headers: { 'Content-Type': 'application/json' },
        body: '{\n  "input": {{ json .input.question }}\n}',
        response: { output: '$.output', trace: { path: '$.steps' } },
        timeout_ms: 120000,
      }
    case 'openai':
      return { base_url: 'https://api.openai.com/v1', model: 'gpt-4o-mini', temperature: 0 }
    case 'anthropic':
      return { base_url: 'https://api.anthropic.com', model: 'claude-sonnet-4-5', max_tokens: 1024, temperature: 0 }
    case 'bedrock':
      return { region: 'us-east-1', model: '', max_tokens: 1024 }
    case 'vertex':
      return { location: 'us-central1', model: 'gemini-2.5-flash', temperature: 0 }
  }
}

const pathsOf = (cfg: Obj): [string, string][] => {
  const out: [string, string][] = []
  const r = cfg.response
  if (!isObj(r)) return out
  for (const [k, v] of Object.entries(r)) {
    if (typeof v === 'string' && v) out.push([`response.${k}`, v])
    else if (isObj(v) && typeof v.path === 'string' && v.path) out.push([`response.${k}.path`, v.path])
  }
  return out
}

/** Config problems that block saving, keyed by dotted path. */
export function validateTarget(kind: TargetKind, config: Obj): Record<string, string> {
  const e: Record<string, string> = {}
  const need = (key: string) => {
    const v = getIn<unknown>(config, key.split('.'))
    if (v === undefined || v === null || String(v).trim() === '') e[key] = 'Required'
  }
  if (isHttpLike(kind)) {
    need('url')
    if (kind === 'agent' && !getIn(config, ['response', 'trace', 'path']) && !getIn(config, ['response', 'tool_calls', 'path'])) e['response.trace.path'] = 'Agents need a trace or tool-calls mapping'
    for (const [k, p] of pathsOf(config)) if (!parsePath(p)) e[k] = 'Not a valid JSONPath (start with $)'
  } else {
    need('model')
    if (kind === 'openai' || kind === 'anthropic') need('base_url')
    if (kind === 'bedrock') {
      need('region')
      need('access_key_id')
      need('secret_access_key')
    }
  }
  return e
}

/** One-line description of where a target points, for lists. */
export function targetEndpoint(kind: TargetKind, config: Obj | undefined): string {
  if (!config) return ''
  if (isHttpLike(kind)) return `${String(config.method ?? 'POST')} ${String(config.url ?? '')}`
  return String(config.model ?? '')
}
