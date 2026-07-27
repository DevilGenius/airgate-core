import { memo, type ReactNode } from 'react';
import { ToolbarMenu, ToolbarMenuItem } from './ToolbarMenu';
import type { SimpleSelectOption } from './SimpleSelect';

interface SimpleMultiSelectProps {
  allLabel: ReactNode;
  ariaLabel: string;
  className?: string;
  fullWidth?: boolean;
  items: SimpleSelectOption[];
  onOpenChange?: (isOpen: boolean) => void;
  onSelectionChange: (keys: string[]) => void;
  selectedKeys: readonly string[];
  selectedLabel?: ReactNode;
  triggerClassName?: string;
}

export const SimpleMultiSelect = memo(function SimpleMultiSelect({
  allLabel,
  ariaLabel,
  className,
  fullWidth = false,
  items,
  onOpenChange,
  onSelectionChange,
  selectedKeys,
  selectedLabel,
  triggerClassName,
}: SimpleMultiSelectProps) {
  const selected = new Set(selectedKeys);
  const selectedItems = items.filter((item) => selected.has(item.key));
  const fallbackLabel = selectedItems.length > 0
    ? selectedItems.map((item) => item.textValue ?? (typeof item.label === 'string' ? item.label : item.key)).join(', ')
    : allLabel;
  const displayLabel = selectedLabel ?? fallbackLabel;

  return (
    <ToolbarMenu
      ariaLabel={ariaLabel}
      className={['ag-simple-select-trigger select__trigger', triggerClassName].filter(Boolean).join(' ')}
      label={displayLabel}
      onOpenChange={onOpenChange}
      rootClassName={['ag-simple-select', fullWidth && 'ag-simple-select--full', className].filter(Boolean).join(' ')}
    >
      {() => (
        <div className="ag-simple-select-popover-content">
          <ToolbarMenuItem
            isSelected={selected.size === 0}
            role="menuitemcheckbox"
            onSelect={() => onSelectionChange([])}
          >
            {allLabel}
          </ToolbarMenuItem>
          {items.map((item) => (
            <ToolbarMenuItem
              key={item.key}
              isDisabled={item.isDisabled}
              isSelected={selected.has(item.key)}
              role="menuitemcheckbox"
              onSelect={() => {
                if (item.isDisabled) return;
                const next = new Set(selected);
                if (next.has(item.key)) {
                  next.delete(item.key);
                } else {
                  next.add(item.key);
                }
                onSelectionChange(Array.from(next));
              }}
            >
              <span className="ag-simple-select-option-copy">
                <span className="ag-simple-select-option-label">{item.label}</span>
                {item.description ? (
                  <span className="ag-simple-select-option-description">{item.description}</span>
                ) : null}
              </span>
            </ToolbarMenuItem>
          ))}
        </div>
      )}
    </ToolbarMenu>
  );
});
