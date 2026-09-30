import { ChevronDown } from 'lucide-react';

export function DropdownIndicator({ className, slot }: { className?: string; slot?: string }) {
  return (
    <ChevronDown
      aria-hidden="true"
      data-slot={slot}
      className={['ag-dropdown-indicator', className].filter(Boolean).join(' ')}
      size={16}
      strokeWidth={2}
    />
  );
}
