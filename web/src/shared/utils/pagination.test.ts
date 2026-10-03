import { describe, expect, it } from 'vitest';
import { getPaginationItems } from './pagination';

describe('stable pagination slots', () => {
  it.each([8, 10, 100, 1000])('keeps seven slots throughout %s pages', (total) => {
    for (let page = 1; page <= total; page += 1) {
      const items = getPaginationItems(page, total);
      expect(items).toHaveLength(7);
      expect(items).toContain(page);
      const numbers = items.filter((item): item is number => typeof item === 'number');
      expect(numbers[0]).toBe(1);
      expect(numbers[numbers.length - 1]).toBe(total);
      expect(numbers).toEqual([...new Set(numbers)].sort((a, b) => a - b));
    }
  });

  it('keeps the first two pages in the same slots', () => {
    expect(getPaginationItems(1, 100)).toEqual(getPaginationItems(2, 100));
  });

  it.each([1, 2, 7])('shows every page for %s total pages', (total) => {
    expect(getPaginationItems(1, total)).toEqual(Array.from({ length: total }, (_, index) => index + 1));
  });
});
