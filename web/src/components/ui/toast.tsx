import { CircleCheck, CircleX, Info, LoaderCircle, TriangleAlert } from 'lucide-react'
import { Toaster as Sonner, toast } from 'sonner'
import { useTheme } from '@/lib/theme'

export { toast }

/** Mount once at the app root. Use `toast.success(…)`, `toast.error(…)`, `toast.promise(…)` anywhere. */
export function Toaster() {
  const { resolved } = useTheme()
  return (
    <Sonner
      theme={resolved}
      position="bottom-right"
      gap={8}
      visibleToasts={4}
      icons={{
        success: <CircleCheck className="size-4 text-pass" />,
        error: <CircleX className="size-4 text-fail" />,
        warning: <TriangleAlert className="size-4 text-warn" />,
        info: <Info className="size-4 text-accent" />,
        loading: <LoaderCircle className="size-4 animate-spin text-accent" />,
      }}
      toastOptions={{
        unstyled: true,
        classNames: {
          toast:
            'flex w-[356px] items-start gap-2.5 rounded-md border border-border bg-surface p-3 text-sm text-text shadow-dialog',
          title: 'font-medium text-text',
          description: 'mt-0.5 text-xs text-text-2',
          icon: 'mt-0.5 flex shrink-0',
          content: 'flex min-w-0 flex-1 flex-col',
          actionButton:
            'ml-auto h-7 shrink-0 rounded-md bg-accent px-2.5 text-sm font-medium text-on-accent hover:bg-accent-hover',
          cancelButton: 'ml-auto h-7 shrink-0 rounded-md border border-border px-2.5 text-sm text-text-2 hover:bg-surface-hover',
          closeButton: 'border-border! bg-surface! text-text-muted!',
        },
      }}
    />
  )
}
