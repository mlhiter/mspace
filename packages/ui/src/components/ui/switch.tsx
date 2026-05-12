import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"

import { cn } from "@mspace/ui/lib/utils"

function Switch({
  className,
  size = "default",
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root> & {
  size?: "sm" | "default"
}) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn(
        "peer group/switch relative inline-flex shrink-0 items-center rounded-full border border-[color:var(--line)] bg-[color:var(--block)] transition-[background-color,border-color,box-shadow,opacity] duration-150 ease-out outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] data-[state=checked]:border-[color:var(--ink)] data-[state=checked]:bg-[color:var(--ink)] data-[state=unchecked]:border-[color:var(--line)] data-[state=unchecked]:bg-[color:var(--block)] data-disabled:cursor-not-allowed data-disabled:opacity-45",
        size === "sm" ? "h-4 w-7" : "h-6 w-10",
        className
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className={cn(
          "pointer-events-none absolute left-0.5 top-1/2 block -translate-y-1/2 rounded-full border border-[color:var(--line)] bg-[color:var(--surface)] shadow-[0_1px_3px_rgba(0,0,0,0.14)] transition-transform duration-150 ease-out data-[state=unchecked]:translate-x-0",
          size === "sm"
            ? "size-3 data-[state=checked]:translate-x-3"
            : "size-5 data-[state=checked]:translate-x-4"
        )}
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
