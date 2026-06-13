import type { ReactNode } from 'react';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type { SelectOption } from './types';

export function MonitorFilterSelect({
  ariaLabel,
  className,
  label,
  onChange,
  options,
  selectedLabel,
  value,
}: {
  ariaLabel: string;
  className?: string;
  label?: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  selectedLabel?: ReactNode;
  value: string;
}) {
  const selected = options.find((item) => item.id === value)?.label ?? options[0]?.label ?? '';
  return (
    <div className={className}>
      <SimpleSelect
        ariaLabel={ariaLabel}
        fullWidth
        items={options.map((item) => ({ key: item.id, label: item.label }))}
        selectedKey={value}
        selectedLabel={selectedLabel ?? (label ? `${label}: ${selected}` : selected)}
        onSelectionChange={onChange}
      />
    </div>
  );
}
