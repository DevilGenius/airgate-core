import type { AccountResp } from '../../../shared/types';

export type BulkEditInitialValues = {
  groupIds: number[];
  maxConcurrency?: number;
  priority?: number;
  priorityMax?: number;
  priorityMin?: number;
  rateMultiplier?: number;
  modelDowngradeThreshold?: number;
};

export type BulkEditSelection = {
  ids: number[];
  initialValues: BulkEditInitialValues;
};

export function orderSelectedAccountIdsByCreatedAt(rows: AccountResp[], selectedIds: number[]) {
  const rowsByID = new Map(rows.map((row) => [row.id, row]));
  return [...selectedIds].sort((leftID, rightID) => {
    const left = rowsByID.get(leftID);
    const right = rowsByID.get(rightID);
    if (!left) return right ? 1 : leftID - rightID;
    if (!right) return -1;
    const createdAtOrder = left.created_at.localeCompare(right.created_at);
    return createdAtOrder || leftID - rightID;
  });
}

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

  const groupIds = firstGroupIds.filter((groupId) => commonGroupIds.has(groupId));
  const priorities = selectedRows
    .map((account) => account.priority)
    .filter((priority) => typeof priority === 'number' && Number.isFinite(priority));
  const hasCompletePriorityRange = selectedRows.length === selectedIds.length && priorities.length === selectedRows.length;
  return {
    groupIds,
    maxConcurrency: getCommonNumber((account) => account.max_concurrency),
    priority: getCommonNumber((account) => account.priority),
    priorityMax: hasCompletePriorityRange ? Math.max(...priorities) : undefined,
    priorityMin: hasCompletePriorityRange ? Math.min(...priorities) : undefined,
    rateMultiplier: getCommonNumber((account) => account.rate_multiplier),
    modelDowngradeThreshold: getCommonNumber((account) => account.model_downgrade_threshold),
  };
}
