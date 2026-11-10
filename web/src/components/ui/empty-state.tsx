import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export type IllustrationName = 'runs' | 'datasets' | 'suites' | 'targets' | 'evaluators' | 'review' | 'search' | 'chart' | 'generic' | 'error'

/** Calm, token-colored line illustrations. Iris accent + neutral strokes, so they work in both themes. */
export function Illustration({ name, className }: { name: IllustrationName; className?: string }) {
  const stroke = 'var(--border-strong)'
  const fill = 'var(--surface)'
  const soft = 'var(--surface-2)'
  const accent = 'var(--accent)'
  const accentSoft = 'var(--accent-subtle)'
  const common = { width: 160, height: 112, viewBox: '0 0 160 112', fill: 'none', 'aria-hidden': true, className }
  // Backdrop: soft dotted grid + base card.
  const backdrop = (
    <>
      <defs>
        <pattern id={`dots-${name}`} width="10" height="10" patternUnits="userSpaceOnUse">
          <circle cx="1" cy="1" r="1" fill={stroke} opacity="0.5" />
        </pattern>
        <radialGradient id={`fade-${name}`} cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="white" />
          <stop offset="100%" stopColor="white" stopOpacity="0" />
        </radialGradient>
        <mask id={`mask-${name}`}>
          <rect width="160" height="112" fill={`url(#fade-${name})`} />
        </mask>
      </defs>
      <rect width="160" height="112" fill={`url(#dots-${name})`} mask={`url(#mask-${name})`} />
    </>
  )
  switch (name) {
    case 'runs':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="28" y="22" width="104" height="70" rx="8" fill={fill} stroke={stroke} />
          {[0, 1, 2].map((i) => (
            <g key={i} transform={`translate(0 ${i * 18})`}>
              <circle cx="42" cy="38" r="4" fill={i === 1 ? accentSoft : soft} stroke={i === 1 ? accent : stroke} />
              <rect x="52" y="35" width={i === 1 ? 40 : 52} height="6" rx="3" fill={soft} />
              <rect x="100" y="35" width="22" height="6" rx="3" fill={i === 1 ? accent : soft} opacity={i === 1 ? 0.9 : 1} />
            </g>
          ))}
          <path d="M120 12l3 6 6 3-6 3-3 6-3-6-6-3 6-3z" fill={accent} opacity="0.85" />
        </svg>
      )
    case 'datasets':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="40" y="30" width="88" height="62" rx="8" fill={soft} stroke={stroke} />
          <rect x="34" y="24" width="88" height="62" rx="8" fill={fill} stroke={stroke} />
          <path d="M34 40h88M64 24v62M94 24v62" stroke={stroke} />
          <rect x="40" y="46" width="18" height="5" rx="2.5" fill={accentSoft} />
          <rect x="70" y="46" width="18" height="5" rx="2.5" fill={soft} />
          <rect x="100" y="46" width="16" height="5" rx="2.5" fill={soft} />
          <rect x="40" y="58" width="14" height="5" rx="2.5" fill={soft} />
          <rect x="70" y="58" width="20" height="5" rx="2.5" fill={accent} opacity="0.85" />
          <rect x="100" y="58" width="12" height="5" rx="2.5" fill={soft} />
          <rect x="40" y="70" width="18" height="5" rx="2.5" fill={soft} />
          <rect x="70" y="70" width="14" height="5" rx="2.5" fill={soft} />
        </svg>
      )
    case 'suites':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="30" y="28" width="44" height="28" rx="6" fill={fill} stroke={stroke} />
          <rect x="86" y="28" width="44" height="28" rx="6" fill={fill} stroke={stroke} />
          <rect x="58" y="70" width="44" height="24" rx="6" fill={accentSoft} stroke={accent} />
          <path d="M52 56v6a6 6 0 006 6h4M108 56v6a6 6 0 01-6 6h-4" stroke={stroke} />
          <rect x="38" y="38" width="20" height="4" rx="2" fill={soft} />
          <rect x="38" y="45" width="28" height="4" rx="2" fill={soft} />
          <rect x="94" y="38" width="24" height="4" rx="2" fill={soft} />
          <rect x="94" y="45" width="16" height="4" rx="2" fill={soft} />
          <path d="M72 82l5 5 9-10" stroke={accent} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )
    case 'targets':
      return (
        <svg {...common}>
          {backdrop}
          <circle cx="80" cy="56" r="34" fill={fill} stroke={stroke} />
          <circle cx="80" cy="56" r="22" fill={soft} stroke={stroke} />
          <circle cx="80" cy="56" r="10" fill={accentSoft} stroke={accent} />
          <circle cx="80" cy="56" r="3" fill={accent} />
          <path d="M118 22l-32 30" stroke={accent} strokeWidth="2" strokeLinecap="round" />
          <path d="M112 20h8v8" stroke={accent} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )
    case 'evaluators':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="34" y="24" width="92" height="66" rx="8" fill={fill} stroke={stroke} />
          {[0.82, 0.64, 0.91].map((v, i) => (
            <g key={i} transform={`translate(0 ${i * 18})`}>
              <rect x="46" y="36" width="34" height="6" rx="3" fill={soft} />
              <rect x="88" y="36" width="28" height="6" rx="3" fill={soft} />
              <rect x="88" y="36" width={28 * v} height="6" rx="3" fill={i === 1 ? stroke : accent} opacity={i === 1 ? 1 : 0.85} />
            </g>
          ))}
        </svg>
      )
    case 'review':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="44" y="30" width="80" height="60" rx="8" fill={soft} stroke={stroke} transform="rotate(4 84 60)" />
          <rect x="38" y="24" width="80" height="60" rx="8" fill={fill} stroke={stroke} />
          <rect x="48" y="36" width="44" height="5" rx="2.5" fill={soft} />
          <rect x="48" y="46" width="58" height="5" rx="2.5" fill={soft} />
          <rect x="48" y="56" width="36" height="5" rx="2.5" fill={soft} />
          {[0, 1, 2, 3, 4].map((i) => (
            <circle key={i} cx={52 + i * 12} cy="72" r="4" fill={i < 4 ? accent : soft} stroke={i < 4 ? 'none' : stroke} opacity={i < 4 ? 0.85 : 1} />
          ))}
        </svg>
      )
    case 'search':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="30" y="30" width="78" height="52" rx="8" fill={fill} stroke={stroke} />
          <rect x="40" y="42" width="40" height="5" rx="2.5" fill={soft} />
          <rect x="40" y="52" width="56" height="5" rx="2.5" fill={soft} />
          <rect x="40" y="62" width="30" height="5" rx="2.5" fill={soft} />
          <circle cx="108" cy="66" r="16" fill={accentSoft} stroke={accent} strokeWidth="2" />
          <path d="M120 78l10 10" stroke={accent} strokeWidth="3" strokeLinecap="round" />
        </svg>
      )
    case 'chart':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="28" y="22" width="104" height="70" rx="8" fill={fill} stroke={stroke} />
          <path d="M40 80h80" stroke={stroke} />
          {[18, 30, 24, 40, 34, 46].map((h, i) => (
            <rect key={i} x={44 + i * 13} y={78 - h} width="8" height={h} rx="2" fill={i === 5 ? accent : soft} stroke={i === 5 ? 'none' : stroke} opacity={i === 5 ? 0.9 : 1} />
          ))}
        </svg>
      )
    case 'error':
      return (
        <svg {...common}>
          {backdrop}
          <rect x="34" y="26" width="92" height="62" rx="8" fill={fill} stroke={stroke} />
          <path d="M34 40h92" stroke={stroke} />
          <circle cx="44" cy="33" r="2" fill={stroke} />
          <circle cx="51" cy="33" r="2" fill={stroke} />
          <path d="M72 54l16 16M88 54L72 70" stroke="var(--fail)" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      )
    default:
      return (
        <svg {...common}>
          {backdrop}
          <rect x="40" y="26" width="80" height="62" rx="10" fill={fill} stroke={stroke} />
          <rect x="52" y="40" width="32" height="6" rx="3" fill={accentSoft} />
          <rect x="52" y="52" width="56" height="5" rx="2.5" fill={soft} />
          <rect x="52" y="62" width="44" height="5" rx="2.5" fill={soft} />
          <path d="M116 18l2.5 5 5 2.5-5 2.5-2.5 5-2.5-5-5-2.5 5-2.5z" fill={accent} opacity="0.85" />
        </svg>
      )
  }
}

export interface EmptyStateProps {
  title: ReactNode
  description?: ReactNode
  illustration?: IllustrationName
  /** Primary action (every empty state should have one). */
  action?: ReactNode
  secondaryAction?: ReactNode
  /** Compact variant for inside cards / tables. */
  compact?: boolean
  className?: string
}

export function EmptyState({ title, description, illustration = 'generic', action, secondaryAction, compact, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center text-center', compact ? 'gap-2 px-4 py-8' : 'gap-3 px-6 py-16', className)}>
      <Illustration name={illustration} className={compact ? 'h-20 w-auto' : undefined} />
      <div className="flex max-w-sm flex-col gap-1">
        <h3 className={cn('font-semibold text-text', compact ? 'text-base' : 'text-md')}>{title}</h3>
        {description && <p className="text-sm text-text-2">{description}</p>}
      </div>
      {(action || secondaryAction) && (
        <div className="mt-2 flex items-center gap-2">
          {action}
          {secondaryAction}
        </div>
      )}
    </div>
  )
}
