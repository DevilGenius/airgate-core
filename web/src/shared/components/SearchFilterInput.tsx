import { memo, useEffect, useRef, useState } from 'react';
import { Input, TextField as HeroTextField } from '@heroui/react';
import { Search } from 'lucide-react';
import { useDebouncedValue } from '../hooks/useDebouncedValue';

interface SearchFilterInputProps {
  ariaLabel: string;
  className?: string;
  debounceMs?: number;
  inputClassName?: string;
  onSearchChange: (value: string) => void;
  placeholder: string;
  value?: string;
}

export const SearchFilterInput = memo(function SearchFilterInput({
  ariaLabel,
  className,
  debounceMs = 250,
  inputClassName = 'pl-9',
  onSearchChange,
  placeholder,
  value = '',
}: SearchFilterInputProps) {
  const [inputValue, setInputValue] = useState(value);
  const debouncedValue = useDebouncedValue(inputValue.trim(), debounceMs);
  const lastEmittedValueRef = useRef(value.trim());

  useEffect(() => {
    setInputValue((current) => (current === value ? current : value));
    lastEmittedValueRef.current = value.trim();
  }, [value]);

  useEffect(() => {
    if (debouncedValue === lastEmittedValueRef.current) return;
    lastEmittedValueRef.current = debouncedValue;
    onSearchChange(debouncedValue);
  }, [debouncedValue, onSearchChange]);

  return (
    <HeroTextField fullWidth className={className} aria-label={ariaLabel}>
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
        <Input
          aria-label={ariaLabel}
          className={inputClassName}
          placeholder={placeholder}
          value={inputValue}
          onChange={(event) => setInputValue(event.target.value)}
        />
      </div>
    </HeroTextField>
  );
});
