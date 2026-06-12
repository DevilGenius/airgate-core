import type { AccountResp } from '../../../shared/types';

export type BulkEditInitialValues = {
  groupIds: number[];
  maxConcurrency?: number;
  priority?: number;
  rateMultiplier?: number;
};

export type BulkEditSelection = {
  ids: number[];
  initialValues: BulkEditInitialValues;
};

export function normalizeAccountGroupIds(value: unknown): number[] {
  if (!Array.isArray(value)) return [];
  return value
    .map((item) => Number(item))
    .filter((item) => Number.isInteger(item) && item > 0);
}

export function getBulkEditInitialValues(rows: AccountResp[], selectedIds: number[]): BulkEditInitialValues {
  if (selectedIds.length === 0) {
    return { groupIds: [] };
  }

  const selectedIdSet = new Set(selectedIds);
  const selectedRows = rows.filter((row) => selectedIdSet.has(row.id));
  const firstSelectedRow = selectedRows[0];
  if (!firstSelectedRow) {
    return { groupIds: [] };
  }

  const firstGroupIds = normalizeAccountGroupIds(firstSelectedRow.group_ids);
  const commonGroupIds = new Set(firstGroupIds);
  for (const row of selectedRows.slice(1)) {
    const rowGroupIds = new Set(normalizeAccountGroupIds(row.group_ids));
    for (const groupId of Array.from(commonGroupIds)) {
      if (!rowGroupIds.has(groupId)) {
        commonGroupIds.delete(groupId);
      }
    }
  }

  const getCommonNumber = (selectValue: (account: AccountResp) => unknown) => {
    const firstValue = selectValue(firstSelectedRow);
    if (typeof firstValue !== 'number' || !Number.isFinite(firstValue)) {
      return undefined;
    }
    return selectedRows.every((row) => selectValue(row) === firstValue) ? firstValue : undefined;
  };

  return {
    groupIds: firstGroupIds.filter((groupId) => commonGroupIds.has(groupId)),
    maxConcurrency: getCommonNumber((account) => account.max_concurrency),
    priority: getCommonNumber((account) => account.priority),
    rateMultiplier: getCommonNumber((account) => account.rate_multiplier),
  };
}
