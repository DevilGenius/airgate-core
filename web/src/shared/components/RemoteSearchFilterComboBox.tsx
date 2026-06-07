import { memo, startTransition, useCallback, useEffect, useMemo, useState } from 'react';
import { keepPreviousData, useQuery, type QueryKey } from '@tanstack/react-query';
import { REMOTE_SEARCH_DEBOUNCE_MS } from '../constants';
import { SearchFilterComboBox, type SearchFilterComboBoxOption } from './SearchFilterComboBox';

interface RemoteSearchFilterComboBoxProps<TItem> {
  ariaLabel: string;
  buildQueryKey: (keyword: string) => QueryKey;
  debounceMs?: number;
  emptyPrompt: string;
  enabled?: boolean;
  loadingLabel?: string;
  mapItemToOption: (item: TItem) => SearchFilterComboBoxOption;
  minSearchLength?: number;
  noDataLabel: string;
  onSelectionChange: (value: string, label: string) => void;
  placeholder: string;
  queryItems: (keyword: string, signal: AbortSignal) => Promise<readonly TItem[]>;
  selectedKey?: string | null;
  selectedLabel?: string;
}

function RemoteSearchFilterComboBoxInner<TItem>({
  ariaLabel,
  buildQueryKey,
  debounceMs = REMOTE_SEARCH_DEBOUNCE_MS,
  emptyPrompt,
  enabled = true,
  loadingLabel,
  mapItemToOption,
  minSearchLength = 1,
  noDataLabel,
  onSelectionChange,
  placeholder,
  queryItems,
  selectedKey,
  selectedLabel,
}: RemoteSearchFilterComboBoxProps<TItem>) {
  const [searchKeyword, setSearchKeyword] = useState('');
  const [internalSelectedLabel, setInternalSelectedLabel] = useState(selectedLabel ?? '');
  const currentSelectedLabel = selectedLabel ?? internalSelectedLabel;
  const searchActive = enabled && searchKeyword.length >= minSearchLength;

  const { data: queryItemsData, isFetching } = useQuery({
    queryKey: buildQueryKey(searchKeyword),
    queryFn: ({ signal }) => queryItems(searchKeyword, signal),
    enabled: searchActive,
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });

  const options = useMemo(
    () => (searchActive ? (queryItemsData ?? []).map(mapItemToOption) : []),
    [mapItemToOption, queryItemsData, searchActive],
  );

  const visibleOptions = useMemo(() => {
    if (!selectedKey || !currentSelectedLabel || options.some((option) => option.id === selectedKey)) {
      return options;
    }
    return [
      {
        id: selectedKey,
        label: currentSelectedLabel,
        description: undefined,
        textValue: currentSelectedLabel,
      },
      ...options,
    ];
  }, [currentSelectedLabel, options, selectedKey]);

  useEffect(() => {
    if (selectedLabel == null) return;
    setInternalSelectedLabel(selectedLabel);
  }, [selectedLabel]);

  useEffect(() => {
    if (!selectedKey) setInternalSelectedLabel('');
  }, [selectedKey]);

  const handleSearchChange = useCallback((value: string) => {
    startTransition(() => {
      setSearchKeyword(value);
    });
  }, []);

  const handleSelectionChange = useCallback((value: string, label: string) => {
    setInternalSelectedLabel(label);
    onSelectionChange(value, label);
  }, [onSelectionChange]);

  return (
    <SearchFilterComboBox
      ariaLabel={ariaLabel}
      debounceMs={debounceMs}
      isLoading={isFetching}
      items={visibleOptions}
      loadingLabel={loadingLabel}
      selectedKey={selectedKey}
      selectedLabel={currentSelectedLabel}
      placeholder={placeholder}
      emptyPrompt={emptyPrompt}
      noDataLabel={noDataLabel}
      onSearchChange={handleSearchChange}
      onSelectionChange={handleSelectionChange}
    />
  );
}

export const RemoteSearchFilterComboBox = memo(RemoteSearchFilterComboBoxInner) as typeof RemoteSearchFilterComboBoxInner;
