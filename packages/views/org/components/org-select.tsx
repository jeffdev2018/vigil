"use client";

import { useState, type ComponentProps } from "react";
import { Check, ChevronDown } from "lucide-react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@multica/ui/components/ui/command";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

type Props = Omit<ComponentProps<"button">, "value" | "onChange" | "children" | "onSelect"> & {
  value: string;
  items: { value: string; label: string }[];
  onValueChange: (value: string) => void;
};

/** Shared primitives for small choices and searchable actor/team lists. */
export function OrgSelect({ value, items, onValueChange, className, disabled, ...props }: Props) {
  const { t } = useT("org");
  const [open, setOpen] = useState(false);
  const label = items.find(item => item.value === value)?.label ?? t($ => $.visual.choose_team);
  if (items.length > 7) return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button variant="outline" role="combobox" aria-expanded={open} disabled={disabled} {...props} className={cn("w-full min-w-0 justify-between font-normal", className)} />}>
        <span className="truncate">{label}</span><ChevronDown className="size-4 shrink-0 text-muted-foreground" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[var(--anchor-width)] min-w-56 max-w-[calc(100vw-2rem)] p-0">
        <Command label={t($ => $.visual.search)}>
          <CommandInput aria-label={t($ => $.visual.search)} placeholder={t($ => $.visual.search)} />
          <CommandList>
            <CommandEmpty>{t($ => $.studio.no_results)}</CommandEmpty>
            {items.map(item => <CommandItem key={item.value} value={`option:${item.value}`} keywords={[item.label]} onSelect={() => { if (!disabled) onValueChange(item.value); setOpen(false); }}>
              <span className="min-w-0 flex-1 whitespace-normal">{item.label}</span>
              {item.value === value && <Check className="size-4 shrink-0" />}
            </CommandItem>)}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
  return (
    <Select items={items} value={value} disabled={disabled} onValueChange={next => { if (next !== null) onValueChange(next); }}>
      <SelectTrigger {...props} className={cn("w-full min-w-0", className)}><SelectValue /></SelectTrigger>
      <SelectContent align="start" alignItemWithTrigger={false} className="max-w-[calc(100vw-2rem)]">
        {items.map(item => <SelectItem key={item.value} value={item.value}><span className="whitespace-normal">{item.label}</span></SelectItem>)}
      </SelectContent>
    </Select>
  );
}
