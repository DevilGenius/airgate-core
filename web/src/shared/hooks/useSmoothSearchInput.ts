import { startTransition, useCallback, useEffect, useRef, useState } from 'react';
import { REMOTE_SEARCH_DEBOUNCE_MS } from '../constants';
import { useDebouncedValue } from './useDebouncedValue';

interface UseSmoothSearchInputOptions {
  debounceMs?: number;
  onSearchChange: (value: string) => void;
  syncValue?: boolean;
  trim?: boolean;
  value?: string;
}

export function useSmoothSearchInput({
  debounceMs = REMOTE_SEARCH_DEBOUNCE_MS,
  onSearchChange,
  syncValue = true,
  trim = true,
  value = '',
}: UseSmoothSearchInputOptions) {
  const normalize = useCallback((nextValue: string) => (trim ? nextValue.trim() : nextValue), [trim]);
  const [inputValue, setInputValue] = useState(value);
  const debouncedValue = useDebouncedValue(normalize(inputValue), debounceMs);
  const lastEmittedValueRef = useRef(normalize(value));

  const emitSearchChange = useCallback((nextValue: string) => {
    const normalized = normalize(nextValue);
    if (normalized === lastEmittedValueRef.current) return;
    lastEmittedValueRef.current = normalized;
    startTransition(() => {
      onSearchChange(normalized);
    });
  }, [normalize, onSearchChange]);

  useEffect(() => {
    if (!syncValue) return;
    setInputValue((current) => (current === value ? current : value));
    lastEmittedValueRef.current = normalize(value);
  }, [normalize, syncValue, value]);

  useEffect(() => {
    emitSearchChange(debouncedValue);
  }, [debouncedValue, emitSearchChange]);

  return {
    emitSearchChange,
    inputValue,
    setInputValue,
  };
}
