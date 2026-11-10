import { Dialog } from '@/components/ui/dialog'
import { Kbd } from '@/components/ui/kbd'
import { modKey } from '@/lib/hotkeys'
import { projectNav, settingsNav } from './nav'

const groups = [
  {
    title: 'General',
    items: [
      { keys: [modKey, 'K'], label: 'Open command palette' },
      { keys: ['?'], label: 'Show keyboard shortcuts' },
      { keys: ['['], label: 'Collapse / expand sidebar' },
    ],
  },
  {
    title: 'Go to',
    items: [...projectNav, settingsNav].map((n) => ({ keys: ['G', n.key.toUpperCase()], label: n.label })),
  },
  {
    title: 'Tables',
    items: [
      { keys: ['J'], label: 'Next row' },
      { keys: ['K'], label: 'Previous row' },
      { keys: ['↵'], label: 'Open row' },
      { keys: ['X'], label: 'Select row' },
    ],
  },
]

export function ShortcutsSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Keyboard shortcuts" size="lg">
      <div className="grid gap-x-8 gap-y-6 sm:grid-cols-2">
        {groups.map((g) => (
          <section key={g.title} className={g.title === 'Go to' ? 'sm:row-span-2' : undefined}>
            <h3 className="mb-2 text-xs font-medium text-text-muted">{g.title}</h3>
            <ul className="divide-y divide-border">
              {g.items.map((it) => (
                <li key={it.label} className="flex h-9 items-center justify-between gap-4 text-sm text-text">
                  {it.label}
                  <span className="flex items-center gap-1">
                    {it.keys.map((k, i) => (
                      <span key={i} className="flex items-center gap-1">
                        {i > 0 && g.title === 'Go to' && <span className="text-xs text-text-muted">then</span>}
                        <Kbd>{k}</Kbd>
                      </span>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </Dialog>
  )
}
