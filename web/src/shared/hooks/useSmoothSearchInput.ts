import { useCallback, useEffect, useRef, useState } from 'react';
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
  const normalizedInputValue = normalize(inputValue);
  const normalizedValue = normalize(value);
  const debouncedValue = useDebouncedValue(normalizedInputValue, debounceMs);
  const lastEmittedValueRef = useRef(normalize(value));
  const lastValueRef = useRef(normalizedValue);
  const hasExternalValueChange = syncValue
    && normalizedValue !== lastValueRef.current
    && normalizedValue !== lastEmittedValueRef.current;

  const emitSearchChange = useCallback((nextValue: string) => {
    const normalized = normalize(nextValue);
    if (normalized === lastEmittedValueRef.current) return;
    lastEmittedValueRef.current = normalized;
    onSearchChange(normalized);
  }, [normalize, onSearchChange]);

  useEffect(() => {
    if (hasExternalValueChange || debouncedValue !== normalizedInputValue) return;
    emitSearchChange(debouncedValue);
  }, [debouncedValue, emitSearchChange, hasExternalValueChange, normalizedInputValue]);

  useEffect(() => {
    const isEmittedValueEcho = normalizedValue === lastEmittedValueRef.current;
    lastValueRef.current = normalizedValue;
    if (!syncValue) return;
    setInputValue((current) => {
      // A delayed parent echo must not replace characters typed after that
      // value was emitted. Genuine external changes still replace the draft.
      if (isEmittedValueEcho && current !== value) return current;
      return current === value ? current : value;
    });
    if (!isEmittedValueEcho) {
      lastEmittedValueRef.current = normalizedValue;
    }
  }, [normalizedValue, syncValue, value]);

  return {
    emitSearchChange,
    inputValue,
    setInputValue,
  };
}
