"use client"

import { Collapsible as CollapsiblePrimitive } from "@base-ui/react/collapsible"

import { cn } from "@multica/ui/lib/utils"

function Collapsible({ ...props }: CollapsiblePrimitive.Root.Props) {
  return <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
}

function CollapsibleTrigger({ ...props }: CollapsiblePrimitive.Trigger.Props) {
  return (
    <CollapsiblePrimitive.Trigger data-slot="collapsible-trigger" {...props} />
  )
}

// Every disclosure in the product — the run timeline's folds, the sidebar's
// groups, a tool-policy section, an issue's detail panels — went from absent
// to present in one frame, so the reader had to re-find their place in a list
// that had silently grown under them. The panel is the one place worth paying
// for that: Base UI measures the content and hands us
// `--collapsible-panel-height`, so the open height is a real value and not a
// JS measure pass, and the two transient attributes mark the frames the panel
// is arriving and leaving. 200ms is the system's `standard` step, and the
// value the one hand-rolled copy of this used before it moved in here.
//
// `overflow-hidden` is what makes the clip work; menus and popovers inside a
// panel render in a portal, so they are not clipped by it. A caller that needs
// its own overflow can still pass it through `className` — tailwind-merge
// keeps the last word.
function CollapsibleContent({
  className,
  ...props
}: CollapsiblePrimitive.Panel.Props) {
  return (
    <CollapsiblePrimitive.Panel
      data-slot="collapsible-content"
      className={cn(
        "h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-out data-ending-style:h-0 data-starting-style:h-0 motion-reduce:transition-none",
        className,
      )}
      {...props}
    />
  )
}

export { Collapsible, CollapsibleTrigger, CollapsibleContent }
