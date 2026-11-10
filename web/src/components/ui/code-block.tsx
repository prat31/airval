import { Check, Copy } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import type { CodeLanguage } from '@/lib/highlight/shiki'
import { IconButton } from './button'
import { Tooltip } from './tooltip'

export function useCopy(timeout = 1400) {
  const [copied, setCopied] = useState(false)
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), timeout)
    } catch {
      /* clipboard unavailable */
    }
  }
  return { copied, copy }
}

export function CopyButton({ value, label = 'Copy', className }: { value: string; label?: string; className?: string }) {
  const { copied, copy } = useCopy()
  return (
    <Tooltip content={copied ? 'Copied' : label}>
      <IconButton label={label} size="sm" onClick={() => copy(value)} className={className}>
        {copied ? <Check className="text-pass" /> : <Copy />}
      </IconButton>
    </Tooltip>
  )
}

export interface CodeBlockProps {
  code: string
  language?: CodeLanguage
  /** Filename or caption shown in the header bar. */
  title?: ReactNode
  showLineNumbers?: boolean
  copyable?: boolean
  maxHeight?: number | string
  className?: string
}

/** Syntax-highlighted code. Shiki loads lazily on first use; plain text renders until then. */
export function CodeBlock({ code, language = 'text', title, showLineNumbers, copyable = true, maxHeight = 420, className }: CodeBlockProps) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    if (language === 'text') return
    import('@/lib/highlight/shiki')
      .then((m) => m.highlight(code, language))
      .then((h) => !cancelled && setHtml(h))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [code, language])

  return (
    <div className={cn('group relative overflow-hidden rounded-md border border-border bg-surface-2', className)}>
      {title && (
        <div className="flex h-9 items-center justify-between border-b border-border pl-3 pr-1.5">
          <span className="truncate font-mono text-xs text-text-2">{title}</span>
          {copyable && <CopyButton value={code} />}
        </div>
      )}
      {copyable && !title && (
        <CopyButton value={code} className="absolute right-1.5 top-1.5 z-10 bg-surface-2 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100" />
      )}
      <div
        className={cn(
          'overflow-auto px-3.5 py-3 font-mono text-xs leading-5 text-text',
          '[&_pre]:bg-transparent! [&_pre]:outline-none [&_code]:block',
          showLineNumbers &&
            '[&_code]:[counter-reset:line] [&_.line]:before:mr-4 [&_.line]:before:inline-block [&_.line]:before:w-6 [&_.line]:before:text-right [&_.line]:before:text-text-muted [&_.line]:before:[counter-increment:line] [&_.line]:before:content-[counter(line)]',
        )}
        style={{ maxHeight }}
      >
        {html ? (
          <div dangerouslySetInnerHTML={{ __html: html }} />
        ) : (
          <pre>
            <code>
              {code.split('\n').map((l, i) => (
                <span key={i} className="line block">
                  {l || ' '}
                </span>
              ))}
            </code>
          </pre>
        )}
      </div>
    </div>
  )
}
