import { Check, ChevronRight } from 'lucide-react'
import { DropdownMenu as Menu } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { menuItem, overlaySurface } from './overlay'

export const DropdownMenu = Menu.Root
export const DropdownMenuTrigger = Menu.Trigger
export const DropdownMenuGroup = Menu.Group
export const DropdownMenuSub = Menu.Sub
export const DropdownMenuRadioGroup = Menu.RadioGroup

export const DropdownMenuContent = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof Menu.Content>>(
  function DropdownMenuContent({ className, sideOffset = 6, align = 'start', ...props }, ref) {
    return (
      <Menu.Portal>
        <Menu.Content
          ref={ref}
          sideOffset={sideOffset}
          align={align}
          collisionPadding={8}
          className={cn(overlaySurface, 'min-w-48 p-1', className)}
          {...props}
        />
      </Menu.Portal>
    )
  },
)

export interface DropdownMenuItemProps extends ComponentPropsWithoutRef<typeof Menu.Item> {
  icon?: ReactNode
  shortcut?: string
  destructive?: boolean
}

export const DropdownMenuItem = forwardRef<HTMLDivElement, DropdownMenuItemProps>(function DropdownMenuItem(
  { className, icon, shortcut, destructive, children, ...props },
  ref,
) {
  return (
    <Menu.Item
      ref={ref}
      className={cn(menuItem, destructive && 'text-fail [&_svg]:text-fail data-[highlighted]:bg-fail-subtle', className)}
      {...props}
    >
      {icon}
      <span className="flex-1 truncate">{children}</span>
      {shortcut && <span className="text-xs tracking-wide text-text-muted">{shortcut}</span>}
    </Menu.Item>
  )
})

export const DropdownMenuRadioItem = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof Menu.RadioItem> & { icon?: ReactNode }>(
  function DropdownMenuRadioItem({ className, icon, children, ...props }, ref) {
    return (
      <Menu.RadioItem ref={ref} className={cn(menuItem, 'pr-8', className)} {...props}>
        {icon}
        <span className="flex-1 truncate">{children}</span>
        <Menu.ItemIndicator className="absolute right-2 flex">
          <Check className="text-accent!" />
        </Menu.ItemIndicator>
      </Menu.RadioItem>
    )
  },
)

export const DropdownMenuCheckboxItem = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof Menu.CheckboxItem>>(
  function DropdownMenuCheckboxItem({ className, children, ...props }, ref) {
    return (
      <Menu.CheckboxItem ref={ref} className={cn(menuItem, 'pl-8', className)} {...props}>
        <Menu.ItemIndicator className="absolute left-2 flex">
          <Check className="text-accent!" />
        </Menu.ItemIndicator>
        {children}
      </Menu.CheckboxItem>
    )
  },
)

export function DropdownMenuLabel({ className, ...props }: ComponentPropsWithoutRef<typeof Menu.Label>) {
  return <Menu.Label className={cn('px-2 pb-1 pt-1.5 text-xs font-medium text-text-muted', className)} {...props} />
}

export function DropdownMenuSeparator({ className, ...props }: ComponentPropsWithoutRef<typeof Menu.Separator>) {
  return <Menu.Separator className={cn('-mx-1 my-1 h-px bg-border', className)} {...props} />
}

export function DropdownMenuSubTrigger({ className, children, icon, ...props }: ComponentPropsWithoutRef<typeof Menu.SubTrigger> & { icon?: ReactNode }) {
  return (
    <Menu.SubTrigger className={cn(menuItem, 'data-[state=open]:bg-surface-hover', className)} {...props}>
      {icon}
      <span className="flex-1">{children}</span>
      <ChevronRight />
    </Menu.SubTrigger>
  )
}

export function DropdownMenuSubContent({ className, ...props }: ComponentPropsWithoutRef<typeof Menu.SubContent>) {
  return (
    <Menu.Portal>
      <Menu.SubContent className={cn(overlaySurface, 'min-w-44 p-1', className)} {...props} />
    </Menu.Portal>
  )
}
