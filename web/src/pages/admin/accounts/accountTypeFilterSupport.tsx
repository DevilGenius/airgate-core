import type { ReactNode } from 'react';
import { NativeSoftChip } from './accountNativeChip';

export type AccountTypeFilterOption = {
  id: string;
  label: string;
  planLabel?: string;
  platformLabel?: string;
};

export function renderAccountTypeFilterOption(option: AccountTypeFilterOption, showOAuthLabel = true): ReactNode {
  if (!option.planLabel) return option.label;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      {option.platformLabel ? <span className="truncate">{option.platformLabel}</span> : null}
      {showOAuthLabel ? <span className="truncate">OAuth</span> : null}
      <NativeSoftChip className="ag-account-type-plan-chip" tone="accent">
        {option.planLabel}
      </NativeSoftChip>
    </span>
  );
}
