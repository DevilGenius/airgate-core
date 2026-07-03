import type { AccountResp } from '../../../shared/types';
import { getAccountGroupPriorities } from './accountDefaults';

export type BulkEditInitialValues = {
  groupIds: number[];
  groupPriorities: Record<number, number>;
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
    return { groupIds: [], groupPriorities: {} };
  }

  const selectedIdSet = new Set(selectedIds);
  const selectedRows = rows.filter((row) => selectedIdSet.has(row.id));
  const firstSelectedRow = selectedRows[0];
  if (!firstSelectedRow) {
    return { groupIds: [], groupPriorities: {} };
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
  return {
    groupIds,
    groupPriorities: getCommonGroupPriorities(selectedRows, groupIds),
    maxConcurrency: getCommonNumber((account) => account.max_concurrency),
    priority: getCommonNumber((account) => account.priority),
    rateMultiplier: getCommonNumber((account) => account.rate_multiplier),
  };
}

function getCommonGroupPriorities(rows: AccountResp[], groupIds: number[]) {
  const result: Record<number, number> = {};
  if (rows.length === 0 || groupIds.length === 0) return result;

  const prioritiesByRow = rows.map((row) => getAccountGroupPriorities(row.extra));
  for (const groupId of groupIds) {
    const firstPriority = prioritiesByRow[0][groupId];
    if (firstPriority == null) continue;
    if (prioritiesByRow.every((priorities) => priorities[groupId] === firstPriority)) {
      result[groupId] = firstPriority;
    }
  }
  return result;
}
