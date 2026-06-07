import { memo } from 'react';
import { REMOTE_SEARCH_DEBOUNCE_MS } from '../constants';
import { SearchFilterInput } from './SearchFilterInput';

interface UsageModelFilterInputProps {
  ariaLabel: string;
  debounceMs?: number;
  onModelChange: (model: string) => void;
  placeholder: string;
  value?: string;
}

export const UsageModelFilterInput = memo(function UsageModelFilterInput({
  ariaLabel,
  debounceMs = REMOTE_SEARCH_DEBOUNCE_MS,
  onModelChange,
  placeholder,
  value = '',
}: UsageModelFilterInputProps) {
  return (
    <SearchFilterInput
      ariaLabel={ariaLabel}
      debounceMs={debounceMs}
      placeholder={placeholder}
      value={value}
      onSearchChange={onModelChange}
    />
  );
});
