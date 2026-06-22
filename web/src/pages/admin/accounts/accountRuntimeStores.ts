import { startTransition, useRef } from 'react';

type SelectionListener = () => void;

export function runAfterInputFrame(work: () => void) {
  if (typeof window === 'undefined') {
    startTransition(work);
    return;
  }
  if (typeof document !== 'undefined' && document.hidden) {
    window.setTimeout(() => startTransition(work), 0);
    return;
  }

  window.requestAnimationFrame(() => {
    window.setTimeout(() => startTransition(work), 0);
  });
}

export function useLatestRef<T>(value: T) {
  const ref = useRef(value);
  ref.current = value;
  return ref;
}

export class AccountSelectionStore {
  private selectedIds = new Set<number>();
  private version = 0;
  private listeners = new Set<SelectionListener>();
  private notifyFrameId: number | null = null;
  private rowInputs = new Map<number, HTMLInputElement>();

  subscribe = (listener: SelectionListener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = () => this.version;

  getSelectedCount = () => this.selectedIds.size;

  registerRowInput(id: number, input: HTMLInputElement | null) {
    if (!input) {
      this.rowInputs.delete(id);
      return;
    }
    this.rowInputs.set(id, input);
    input.checked = this.selectedIds.has(id);
  }

  has(id: number) {
    return this.selectedIds.has(id);
  }

  getSelectedIds() {
    return Array.from(this.selectedIds);
  }

  countVisible(ids: number[]) {
    let count = 0;
    for (const id of ids) {
      if (this.selectedIds.has(id)) count += 1;
    }
    return count;
  }

  setRow(id: number, isSelected: boolean) {
    const alreadySelected = this.selectedIds.has(id);
    if (alreadySelected === isSelected) return 0;

    if (isSelected) {
      this.selectedIds.add(id);
    } else {
      this.selectedIds.delete(id);
    }
    this.syncRowInputs([id]);
    this.notify();
    return 1;
  }

  setRows(ids: number[], isSelected: boolean) {
    const changedIds: number[] = [];
    for (const id of ids) {
      const alreadySelected = this.selectedIds.has(id);
      if (alreadySelected === isSelected) continue;
      if (isSelected) {
        this.selectedIds.add(id);
      } else {
        this.selectedIds.delete(id);
      }
      changedIds.push(id);
    }
    if (changedIds.length > 0) {
      this.syncRowInputs(changedIds);
      this.notify();
    }
    return changedIds.length;
  }

  clear() {
    if (this.selectedIds.size === 0) return 0;
    const changedIds = Array.from(this.selectedIds);
    this.selectedIds.clear();
    this.syncRowInputs(changedIds);
    this.notify();
    return changedIds.length;
  }

  private syncRowInputs(changedIds: number[]) {
    for (const id of changedIds) {
      const input = this.rowInputs.get(id);
      if (input) {
        input.checked = this.selectedIds.has(id);
      }
    }
  }

  private notify() {
    this.version += 1;
    if (typeof window === 'undefined') {
      this.listeners.forEach((listener) => listener());
      return;
    }
    if (this.notifyFrameId != null) return;
    this.notifyFrameId = window.requestAnimationFrame(() => {
      this.notifyFrameId = null;
      this.listeners.forEach((listener) => listener());
    });
  }
}

export class AccountCapacityStore {
  private counts = new Map<number, number>();
  private listeners = new Map<number, Set<SelectionListener>>();

  subscribe = (id: number, listener: SelectionListener) => {
    let listeners = this.listeners.get(id);
    if (!listeners) {
      listeners = new Set();
      this.listeners.set(id, listeners);
    }
    listeners.add(listener);
    return () => {
      listeners?.delete(listener);
      if (listeners?.size === 0) {
        this.listeners.delete(id);
      }
    };
  };

  getCurrent(id: number, fallback: number) {
    return this.counts.get(id) ?? fallback;
  }

  setCount(id: number, count: number) {
    if (!Number.isFinite(id) || !Number.isFinite(count)) return;
    const normalizedCount = Math.max(0, Math.trunc(count));
    if (this.counts.get(id) === normalizedCount) return;
    this.counts.set(id, normalizedCount);
    this.listeners.get(id)?.forEach((listener) => listener());
  }

  setMany(nextCounts: Iterable<[number, number]>) {
    const changedIds: number[] = [];
    for (const [id, count] of nextCounts) {
      if (!Number.isFinite(id) || !Number.isFinite(count)) continue;
      const normalizedCount = Math.max(0, Math.trunc(count));
      if (this.counts.get(id) === normalizedCount) continue;
      this.counts.set(id, normalizedCount);
      changedIds.push(id);
    }
    for (const id of changedIds) {
      this.listeners.get(id)?.forEach((listener) => listener());
    }
  }

  setCounts(nextCounts: Record<string, number>) {
    const changedIds: number[] = [];
    const nextIds = new Set<number>();
    for (const [rawId, count] of Object.entries(nextCounts)) {
      const id = Number(rawId);
      if (!Number.isFinite(id)) continue;
      nextIds.add(id);
      if (this.counts.get(id) === count) continue;
      this.counts.set(id, count);
      changedIds.push(id);
    }
    for (const id of Array.from(this.counts.keys())) {
      if (nextIds.has(id)) continue;
      this.counts.delete(id);
      changedIds.push(id);
    }
    for (const id of changedIds) {
      this.listeners.get(id)?.forEach((listener) => listener());
    }
  }
}
