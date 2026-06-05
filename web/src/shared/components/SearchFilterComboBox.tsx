import { memo, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { Input } from '@heroui/react';
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
  debounceMs = 120,
  emptyPrompt,
  items,
  noDataLabel,
  onSearchChange,
  onSelectionChange,
  placeholder,
  selectedKey,
  selectedLabel = '',
}: SearchFilterComboBoxProps) {
  const comboBoxId = useId();
  const [inputValue, setInputValue] = useState(selectedLabel);
  const [isOpen, setIsOpen] = useState(false);
  const debouncedValue = useDebouncedValue(inputValue.trim(), debounceMs);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const lastEmittedValueRef = useRef(selectedLabel.trim());

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      setIsOpen(false);
    };
    const handleFocusIn = (event: FocusEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      setIsOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setIsOpen(false);
    };
    const handleComboBoxOpen = (event: Event) => {
      const detail = (event as CustomEvent<string>).detail;
      if (detail !== comboBoxId) setIsOpen(false);
    };

    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('focusin', handleFocusIn);
    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('ag-search-combobox-open', handleComboBoxOpen);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('focusin', handleFocusIn);
      document.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('ag-search-combobox-open', handleComboBoxOpen);
    };
  }, [comboBoxId, isOpen]);

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

  const openDropdown = () => {
    document.dispatchEvent(new CustomEvent('ag-search-combobox-open', { detail: comboBoxId }));
    setIsOpen(true);
  };

  const handleInputChange = (value: string) => {
    setInputValue(value);
    openDropdown();
    if (!value) {
      lastEmittedValueRef.current = '';
      onSearchChange('');
      onSelectionChange('', '');
      return;
    }
    if (selectedKey && value !== selectedLabel) {
      onSelectionChange('', '');
    }
  };
  const handleSelect = (value: string) => {
    const option = items.find((item) => item.id === value);
    const label = option?.label ? String(option.label) : '';
    setInputValue(label);
    setIsOpen(false);
    lastEmittedValueRef.current = label.trim();
    onSelectionChange(value, label);
    onSearchChange(label.trim());
  };

  return (
    <div ref={rootRef} className="ag-search-combobox" data-open={isOpen ? 'true' : undefined}>
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
        <Input
          aria-label={ariaLabel}
          className="ag-search-combobox-input"
          placeholder={placeholder}
          value={inputValue}
          onChange={(event) => handleInputChange(event.target.value)}
          onFocus={openDropdown}
        />
      </div>
      {isOpen ? (
        <div className="ag-search-combobox-popover">
          <div className="ag-search-combobox-list" role="listbox" aria-label={ariaLabel}>
            {items.length === 0 ? (
              <div className="ag-search-combobox-empty">
                {inputValue.trim() ? noDataLabel : emptyPrompt}
              </div>
            ) : (
              items.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  aria-selected={selectedKey === item.id}
                  className="ag-search-combobox-item"
                  role="option"
                  onClick={() => handleSelect(item.id)}
                >
                  <span className="ag-search-combobox-item-label">{item.label}</span>
                  {item.description ? (
                    <span className="ag-search-combobox-item-description">{item.description}</span>
                  ) : null}
                </button>
              ))
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
});
