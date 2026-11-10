import { Command } from 'cmdk'
import { CornerDownLeft, Search } from 'lucide-react'
import { Dialog as DialogPrimitive } from 'radix-ui'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { DialogOverlay } from './dialog'
import { Kbd } from './kbd'

export interface CommandItem {
  id: string
  label: string
  icon?: ReactNode
  /** Right-aligned hint (e.g. "G R" or a project name). */
  hint?: string
  shortcut?: string[]
  keywords?: string[]
  onSelect: () => void
}

export interface CommandGroup {
  heading: string
  items: CommandItem[]
}

export interface CommandPaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  groups: CommandGroup[]
  placeholder?: string
  /** Controlled search value (optional). */
  search?: string
  onSearchChange?: (s: string) => void
}

/** ⌘K palette: jump to anything, run actions. */
export function CommandPalette({ open, onOpenChange, groups, placeholder = 'Type a command or search…', search, onSearchChange }: CommandPaletteProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogOverlay />
        <DialogPrimitive.Content
          className={cn(
            'fixed left-1/2 top-[14vh] z-50 w-[calc(100vw-32px)] max-w-[600px] -translate-x-1/2 overflow-hidden',
            'rounded-lg border border-border bg-surface shadow-dialog animate-in focus:outline-none',
          )}
        >
          <DialogPrimitive.Title className="sr-only">Command palette</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">Search for pages and actions</DialogPrimitive.Description>
          <Command loop label="Command palette">
            <div className="flex items-center gap-2.5 border-b border-border px-4">
              <Search className="size-4 shrink-0 text-text-muted" />
              <Command.Input
                autoFocus
                value={search}
                onValueChange={onSearchChange}
                placeholder={placeholder}
                className="h-12 w-full bg-transparent text-base text-text outline-none placeholder:text-text-muted"
              />
              <Kbd>Esc</Kbd>
            </div>
            <Command.List className="max-h-[min(420px,60vh)] scroll-py-2 overflow-y-auto p-2">
              <Command.Empty className="px-3 py-10 text-center text-sm text-text-muted">No results found.</Command.Empty>
              {groups.map((g) => (
                <Command.Group
                  key={g.heading}
                  heading={g.heading}
                  className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-text-muted"
                >
                  {g.items.map((it) => (
                    <Command.Item
                      key={it.id}
                      value={`${g.heading} ${it.label}`}
                      keywords={it.keywords}
                      onSelect={() => {
                        onOpenChange(false)
                        it.onSelect()
                      }}
                      className={cn(
                        'group flex h-9 cursor-default select-none items-center gap-2.5 rounded-md px-2 text-sm text-text outline-none',
                        'data-[selected=true]:bg-surface-hover [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-text-muted',
                      )}
                    >
                      {it.icon}
                      <span className="flex-1 truncate">{it.label}</span>
                      {it.hint && <span className="truncate text-xs text-text-muted">{it.hint}</span>}
                      {it.shortcut && (
                        <span className="flex gap-0.5">
                          {it.shortcut.map((k) => (
                            <Kbd key={k}>{k}</Kbd>
                          ))}
                        </span>
                      )}
                      <CornerDownLeft className="hidden size-3.5! group-data-[selected=true]:block" />
                    </Command.Item>
                  ))}
                </Command.Group>
              ))}
            </Command.List>
            <div className="flex items-center gap-4 border-t border-border bg-surface-2 px-4 py-2 text-xs text-text-muted">
              <span className="flex items-center gap-1.5">
                <Kbd>↑</Kbd>
                <Kbd>↓</Kbd> navigate
              </span>
              <span className="flex items-center gap-1.5">
                <Kbd>↵</Kbd> open
              </span>
              <span className="ml-auto flex items-center gap-1.5">
                <Kbd>?</Kbd> shortcuts
              </span>
            </div>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
