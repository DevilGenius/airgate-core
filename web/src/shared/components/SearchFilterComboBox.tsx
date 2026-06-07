import { memo, useCallback, useEffect, useId, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { Input } from '@heroui/react';
import { Search } from 'lucide-react';
import { REMOTE_SEARCH_DEBOUNCE_MS } from '../constants';
import { useSmoothSearchInput } from '../hooks/useSmoothSearchInput';

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
  isLoading?: boolean;
  items: SearchFilterComboBoxOption[];
  loadingLabel?: string;
  noDataLabel: string;
  onSearchChange: (value: string) => void;
  onSelectionChange: (value: string, label: string) => void;
  placeholder: string;
  selectedKey?: string | null;
  selectedLabel?: string;
}

export const SearchFilterComboBox = memo(function SearchFilterComboBox({
  ariaLabel,
  debounceMs = REMOTE_SEARCH_DEBOUNCE_MS,
  emptyPrompt,
  isLoading = false,
  items,
  loadingLabel = 'Loading...',
  noDataLabel,
  onSearchChange,
  onSelectionChange,
  placeholder,
  selectedKey,
  selectedLabel = '',
}: SearchFilterComboBoxProps) {
  const comboBoxId = useId();
  const [isOpen, setIsOpen] = useState(false);
  const { emitSearchChange, inputValue, setInputValue } = useSmoothSearchInput({
    debounceMs,
    onSearchChange,
    syncValue: !!selectedKey,
    value: selectedLabel,
  });
  const isOpenRef = useRef(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  const closeDropdown = useCallback(() => {
    if (!isOpenRef.current) return;
    isOpenRef.current = false;
    setIsOpen(false);
  }, []);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      closeDropdown();
    };
    const handleFocusIn = (event: FocusEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      closeDropdown();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeDropdown();
    };
    const handleComboBoxOpen = (event: Event) => {
      const detail = (event as CustomEvent<string>).detail;
      if (detail !== comboBoxId) closeDropdown();
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
  }, [closeDropdown, comboBoxId, isOpen]);

  const openDropdown = useCallback(() => {
    if (isOpenRef.current) return;
    document.dispatchEvent(new CustomEvent('ag-search-combobox-open', { detail: comboBoxId }));
    isOpenRef.current = true;
    setIsOpen(true);
  }, [comboBoxId]);

  const handleInputPointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    openDropdown();
  }, [openDropdown]);

  const handleInputChange = (value: string) => {
    setInputValue(value);
    openDropdown();
    if (!value) {
      emitSearchChange('');
      onSelectionChange('', '');
      return;
    }
    if (selectedKey && value !== selectedLabel) {
      onSelectionChange('', '');
    }
    if (debounceMs <= 0) {
      emitSearchChange(value);
    }
  };

  const handleSelect = (value: string) => {
    const option = items.find((item) => item.id === value);
    const label = option?.label ? String(option.label) : '';
    setInputValue(label);
    isOpenRef.current = false;
    setIsOpen(false);
    onSelectionChange(value, label);
    emitSearchChange(label);
  };

  const emptyStateLabel = isLoading && inputValue.trim() ? loadingLabel : (inputValue.trim() ? noDataLabel : emptyPrompt);

  return (
    <div ref={rootRef} className="ag-search-combobox" data-open={isOpen ? 'true' : undefined}>
      <div className="relative" onPointerDownCapture={handleInputPointerDown}>
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
      <div className="ag-search-combobox-popover" hidden={!isOpen}>
        <div className="ag-search-combobox-list" role="listbox" aria-label={ariaLabel}>
          {items.length === 0 ? (
            <div className="ag-search-combobox-empty">
              {emptyStateLabel}
            </div>
          ) : (
            items.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-selected={selectedKey === item.id}
                className="ag-search-combobox-item"
                role="option"
                onClick={(event) => {
                  if (event.detail !== 0) return;
                  handleSelect(item.id);
                }}
                onPointerDown={(event) => {
                  if (event.button !== 0) return;
                  event.preventDefault();
                  handleSelect(item.id);
                }}
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
    </div>
  );
});
