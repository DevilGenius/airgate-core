import { memo, type ReactNode } from 'react';
import { ToolbarMenu, ToolbarMenuItem } from './ToolbarMenu';

export interface SimpleSelectOption {
  description?: ReactNode;
  isDisabled?: boolean;
  key: string;
  label: ReactNode;
  textValue?: string;
}

interface SimpleSelectProps {
  ariaLabel: string;
  className?: string;
  fullWidth?: boolean;
  isDisabled?: boolean;
  items: SimpleSelectOption[];
  itemClassName?: string;
  onSelectionChange: (key: string) => void;
  placeholder?: ReactNode;
  popoverClassName?: string;
  selectedKey?: string | number | null;
  selectedLabel?: ReactNode;
  triggerClassName?: string;
}

export const SimpleSelect = memo(function SimpleSelect({
  ariaLabel,
  className,
  fullWidth = false,
  isDisabled = false,
  items,
  itemClassName,
  onSelectionChange,
  placeholder,
  popoverClassName,
  selectedKey,
  selectedLabel,
  triggerClassName,
}: SimpleSelectProps) {
  const selectedKeyString = selectedKey == null ? '' : String(selectedKey);
  const selectedOption = items.find((item) => item.key === selectedKeyString);
  const displayLabel = selectedLabel ?? selectedOption?.label ?? placeholder ?? '';

  return (
    <ToolbarMenu
      ariaLabel={ariaLabel}
      className={['ag-simple-select-trigger select__trigger', triggerClassName].filter(Boolean).join(' ')}
      disabled={isDisabled}
      label={displayLabel}
      rootClassName={['ag-simple-select', fullWidth && 'ag-simple-select--full', className].filter(Boolean).join(' ')}
    >
      {(close) => (
        <div className={['ag-simple-select-popover-content', popoverClassName].filter(Boolean).join(' ')}>
          {items.map((item) => (
            <ToolbarMenuItem
              key={item.key}
              className={itemClassName}
              isDisabled={item.isDisabled}
              isSelected={item.key === selectedKeyString}
              role="menuitemradio"
              onSelect={() => {
                if (item.isDisabled) return;
                onSelectionChange(item.key);
                close();
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
