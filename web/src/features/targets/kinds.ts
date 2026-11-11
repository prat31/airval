import { Bot, Cloud, Globe, Network, Sparkles, Waypoints, type LucideIcon } from 'lucide-react'
import type { Obj, S } from '../shared/api'
import { getIn, isObj } from '../shared/obj'
import { parsePath } from '../shared/jsonpath'

export type TargetKind = S['TargetKind']

export interface KindMeta {
  label: string
  description: string
  icon: LucideIcon
  /** Which config section holds the connection settings. */
  section: 'http' | 'openai' | 'anthropic' | 'bedrock' | 'vertex'
}

export const kindOrder: TargetKind[] = ['http', 'openai', 'anthropic', 'bedrock', 'vertex', 'agent']

export const kindMeta: Record<TargetKind, KindMeta> = {
  http: { label: 'HTTP endpoint', description: 'Any REST or streaming API. Map the response with JSONPath.', icon: Globe, section: 'http' },
  openai: { label: 'OpenAI-compatible', description: 'OpenAI, Azure OpenAI, Ollama, vLLM, Together, Groq…', icon: Sparkles, section: 'openai' },
  anthropic: { label: 'Anthropic', description: 'Claude via the Messages API, with tools and system prompt.', icon: Bot, section: 'anthropic' },
  bedrock: { label: 'AWS Bedrock', description: 'Foundation models on Bedrock with access-key secrets.', icon: Cloud, section: 'bedrock' },
  vertex: { label: 'Google Vertex AI', description: 'Gemini and partner models with a service account.', icon: Network, section: 'vertex' },
  agent: { label: 'Agent (HTTP)', description: 'An HTTP agent that also returns a trace: steps, tool calls, retrieved docs.', icon: Waypoints, section: 'http' },
}

export const isLLM = (k: TargetKind) => k === 'openai' || k === 'anthropic' || k === 'bedrock' || k === 'vertex'
export const isHttpLike = (k: TargetKind) => k === 'http' || k === 'agent'

export function defaultConfig(kind: TargetKind): Obj {
  switch (kind) {
    case 'http':
      return {
        http: {
          method: 'POST',
          url: '',
          headers: { 'Content-Type': 'application/json' },
          body: '{\n  "question": "{{ .input.question }}"\n}',
          response: { output: '$.answer' },
          timeout_ms: 60000,
          retries: 2,
        },
      }
    case 'agent':
      return {
        http: {
          method: 'POST',
          url: '',
          headers: { 'Content-Type': 'application/json' },
          body: '{\n  "input": "{{ .input.question }}"\n}',
          response: { output: '$.output', trace: '$.steps', tool_calls: '$.tool_calls' },
          timeout_ms: 120000,
          retries: 1,
        },
      }
    case 'openai':
      return { openai: { base_url: 'https://api.openai.com/v1', model: 'gpt-4o-mini', temperature: 0 } }
    case 'anthropic':
      return { anthropic: { model: 'claude-sonnet-4-5', max_tokens: 1024, temperature: 0 } }
    case 'bedrock':
      return { bedrock: { region: 'us-east-1', model_id: '', max_tokens: 1024 } }
    case 'vertex':
      return { vertex: { location: 'us-central1', model: 'gemini-2.5-flash', temperature: 0 } }
  }
}

export function sectionOf(kind: TargetKind) {
  return kindMeta[kind].section
}

/** Config problems that block saving, keyed by dotted path. */
export function validateTarget(kind: TargetKind, config: Obj): Record<string, string> {
  const e: Record<string, string> = {}
  const sec = sectionOf(kind)
  const need = (path: string[], label = 'Required') => {
    const v = getIn<unknown>(config, [sec, ...path])
    if (v === undefined || v === null || String(v).trim() === '') e[[sec, ...path].join('.')] = label
  }
  if (isHttpLike(kind)) {
    need(['url'])
    const out = getIn<string>(config, ['http', 'response', 'output'], '')
    if (!out) e['http.response.output'] = 'Map where the answer lives, e.g. $.answer'
  } else if (kind === 'openai') {
    need(['model'])
    need(['base_url'])
  } else if (kind === 'anthropic') {
    need(['model'])
  } else if (kind === 'bedrock') {
    need(['region'])
    need(['model_id'])
  } else if (kind === 'vertex') {
    need(['project'])
    need(['location'])
    need(['model'])
  }
  const resp = getIn(config, ['http', 'response'])
  if (isHttpLike(kind) && isObj(resp)) {
    for (const [k, v] of Object.entries(resp)) {
      if (typeof v === 'string' && v && !parsePath(v)) e[`http.response.${k}`] = 'Not a valid JSONPath (start with $)'
    }
  }
  return e
}

/** One-line description of where a target points, for lists. */
export function targetEndpoint(kind: TargetKind, config: Obj | undefined): string {
  if (!config) return ''
  if (isHttpLike(kind)) {
    const h = config.http as Obj | undefined
    return h ? `${String(h.method ?? 'POST')} ${String(h.url ?? '')}` : ''
  }
  const sec = config[sectionOf(kind)] as Obj | undefined
  return String(sec?.model ?? sec?.model_id ?? '')
}
