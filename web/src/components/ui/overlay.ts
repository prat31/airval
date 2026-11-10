/** Shared overlay surface classes (popover, menu, select, combobox). */
export const overlaySurface =
  'z-50 overflow-hidden rounded-md border border-border bg-surface text-text shadow-popover animate-in outline-none'

export const menuItem = [
  'relative flex h-8 cursor-default select-none items-center gap-2 rounded-sm px-2 text-sm text-text outline-none',
  'data-[highlighted]:bg-surface-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
  '[&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-text-muted',
].join(' ')
