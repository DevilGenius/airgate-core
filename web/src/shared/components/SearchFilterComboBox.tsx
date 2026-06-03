import { memo, useEffect, useRef, useState, type ReactNode } from 'react';
import { ComboBox, Input, ListBox } from '@heroui/react';
import { Search } from 'lucide-react';
import { useDebouncedValue } from '../hooks/useDebouncedValue';

export interface SearchFilterComboBoxOption {
  description?: ReactNode;
  id: string;
  label: ReactNode;
  textValue: string;
}

interface SearchFilterComboBoxProps {
  ariaLabel: string;
  debounceMs?: number;
  emptyPrompt: string;
  items: SearchFilterComboBoxOption[];
  noDataLabel: string;
  onSearchChange: (value: string) => void;
  onSelectionChange: (value: string, label: string) => void;
  placeholder: string;
  selectedKey?: string | null;
  selectedLabel?: string;
}

export const SearchFilterComboBox = memo(function SearchFilterComboBox({
  ariaLabel,
  debounceMs = 250,
  emptyPrompt,
  items,
  noDataLabel,
  onSearchChange,
  onSelectionChange,
  placeholder,
  selectedKey,
  selectedLabel = '',
}: SearchFilterComboBoxProps) {
  const [inputValue, setInputValue] = useState(selectedLabel);
  const debouncedValue = useDebouncedValue(inputValue.trim(), debounceMs);
  const lastEmittedValueRef = useRef(selectedLabel.trim());

  useEffect(() => {
    if (!selectedKey) return;
    setInputValue((current) => (current === selectedLabel ? current : selectedLabel));
    lastEmittedValueRef.current = selectedLabel.trim();
  }, [selectedKey, selectedLabel]);

  useEffect(() => {
    if (debouncedValue === lastEmittedValueRef.current) return;
    lastEmittedValueRef.current = debouncedValue;
    onSearchChange(debouncedValue);
  }, [debouncedValue, onSearchChange]);

  return (
    <ComboBox
      aria-label={ariaLabel}
      allowsEmptyCollection
      fullWidth
      inputValue={inputValue}
      items={items}
      menuTrigger="focus"
      selectedKey={selectedKey ?? null}
      onInputChange={(value) => {
        setInputValue(value);
        if (!value) {
          lastEmittedValueRef.current = '';
          onSearchChange('');
          onSelectionChange('', '');
          return;
        }
        if (selectedKey && value !== selectedLabel) {
          onSelectionChange('', '');
        }
      }}
      onSelectionChange={(key) => {
        const value = key == null ? '' : String(key);
        const option = items.find((item) => item.id === value);
        const label = option?.label ? String(option.label) : '';
        setInputValue(label);
        lastEmittedValueRef.current = label.trim();
        onSelectionChange(value, label);
        onSearchChange(label.trim());
      }}
    >
      <ComboBox.InputGroup className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
        <Input className="pl-9" placeholder={placeholder} />
      </ComboBox.InputGroup>
      <ComboBox.Popover>
        <ListBox
          items={items}
          renderEmptyState={() => (
            <div className="px-3 py-6 text-center text-xs text-text-tertiary">
              {inputValue.trim() ? noDataLabel : emptyPrompt}
            </div>
          )}
        >
          {(item) => (
            <ListBox.Item id={item.id} textValue={item.textValue}>
              <div className="min-w-0">
                <div className="truncate">{item.label}</div>
                {item.description ? (
                  <div className="truncate text-xs text-text-tertiary">{item.description}</div>
                ) : null}
              </div>
            </ListBox.Item>
          )}
        </ListBox>
      </ComboBox.Popover>
    </ComboBox>
  );
});
