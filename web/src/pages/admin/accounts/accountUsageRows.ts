// Pure helpers for grouping account usage windows into rendered rows.
//
// Extracted from useAccountTableColumns.tsx so the grouping/ordering logic can be
// unit-tested without pulling in React. Behavior is unchanged except that
// buildWindowRows now orders groups deterministically (see below).

import type { AccountUsageWindow } from './accountUsageSupport';

export type UsageWindowRow = { id: string; window: AccountUsageWindow };

export function normalizeWindowToken(value?: string) {
  return value?.trim().toLowerCase().replace(/_/g, '-') || '';
}

export function getWindowSlot(w: AccountUsageWindow) {
  const key = w.key || '';
  const label = w.label || '';
  const normalizedKey = normalizeWindowToken(key);
  const normalizedLabel = normalizeWindowToken(label);
  const slot = normalizeWindowToken(w.slot)
    || (normalizedKey.includes(':7d') || normalizedKey === '7d' || normalizedLabel.startsWith('7d') ? '7d'
      : normalizedKey === 'monthly' || normalizedKey.includes('monthly') || normalizedLabel.includes('monthly') ? 'monthly'
        : '5h');
  const group = w.group?.trim()
    || (key.startsWith('model:') ? key.replace(/^model:(5h|7d):/, 'model:') : 'base');
  return { group, slot };
}

export function getWindowGroupLabel(group: string, slot: string, label: string) {
  const labelParts = label.trim().split(/\s+/);
  if (labelParts.length > 1 && normalizeWindowToken(labelParts[0]) === slot) {
    return labelParts.slice(1).join(' ');
  }
  const rawGroup = group.replace(/^model:/, '').trim();
  if (!rawGroup || rawGroup === 'base') return '';
  const parts = rawGroup.split(/[-\s:]+/).filter(Boolean);
  return parts[parts.length - 1] ?? rawGroup;
}

export function getWindowDisplay(w: AccountUsageWindow) {
  const { group, slot } = getWindowSlot(w);
  const explicitLabel = w.display_label?.trim();
  const fallbackLabel = explicitLabel || slot || w.label;
  if (group !== 'base' && slot) {
    const groupLabel = getWindowGroupLabel(group, slot, w.label || '');
    if (groupLabel && (!explicitLabel || normalizeWindowToken(explicitLabel) === slot)) {
      return {
        label: `${slot}${groupLabel.charAt(0).toUpperCase()}`,
        title: `${slot} ${groupLabel}`,
      };
    }
  }
  return {
    label: fallbackLabel,
    title: w.label || fallbackLabel,
  };
}

// Group order must be stable and independent of which slot of each group is
// currently present. Otherwise, when a group's 5h window expires/disappears, that
// group would be re-inserted only at its 7d window — which sorts after every other
// group's 5h window — pushing its 7d bar to the bottom of the cell ("7d 跑到最下面").
// Order: the base group first, then model groups alphabetically (mirrors the group
// ordering in sortUsageWindows / usage_contract.go).
function groupOrderRank(id: string) {
  return id === 'base' ? 0 : 1;
}

export function buildWindowRows(items: AccountUsageWindow[]): UsageWindowRow[] {
  const groups: Array<{ id: string; five?: AccountUsageWindow; seven?: AccountUsageWindow; other: AccountUsageWindow[] }> = [];
  const groupMap = new Map<string, { id: string; five?: AccountUsageWindow; seven?: AccountUsageWindow; other: AccountUsageWindow[] }>();

  for (const item of items) {
    const { group, slot } = getWindowSlot(item);
    let bucket = groupMap.get(group);
    if (!bucket) {
      bucket = { id: group, other: [] };
      groupMap.set(group, bucket);
      groups.push(bucket);
    }
    if (slot === '7d') bucket.seven = item;
    else if (slot === '5h') bucket.five = item;
    else bucket.other.push(item);
  }

  groups.sort((left, right) => {
    const leftRank = groupOrderRank(left.id);
    const rightRank = groupOrderRank(right.id);
    if (leftRank !== rightRank) return leftRank - rightRank;
    return left.id.localeCompare(right.id);
  });

  return groups.flatMap((group) => {
    const rows: UsageWindowRow[] = [];
    if (group.five) {
      rows.push({ id: `${group.id}:5h`, window: group.five });
    }
    if (group.seven) {
      rows.push({ id: `${group.id}:7d`, window: group.seven });
    }
    for (const window of group.other) {
      const { slot } = getWindowSlot(window);
      rows.push({ id: `${group.id}:${window.key || slot}:${rows.length}`, window });
    }
    return rows;
  });
}

export function countUsageWindowGroups(windows: AccountUsageWindow[]): number {
  const groups = new Set<string>();
  for (const window of windows) {
    groups.add(getWindowSlot(window).group);
  }
  return groups.size;
}

// A "Pro" account (Claude Pro/Max) has model-specific window groups on top of the
// base group, so it renders up to four rows (base 5h/7d + model 5h/7d). When its
// 5h windows expire only the 7d rows remain — but the distinct group count stays
// >= 2, because the 7d windows persist for ~7 days. Keying the expanded (4-row)
// height on the group count instead of the live row count keeps such rows at a
// stable height rather than collapsing 6rem -> 3rem ("height drift") every time the
// 5h windows roll over. Single-group (free) accounts with >2 windows (e.g.
// 5h/7d/monthly) still expand via the length check.
export function shouldExpandUsageWindows(windows: AccountUsageWindow[] | undefined): boolean {
  if (!Array.isArray(windows)) return false;
  return windows.length > 2 || countUsageWindowGroups(windows) >= 2;
}
