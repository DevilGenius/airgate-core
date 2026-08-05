import { describe, expect, it } from 'vitest';
import {
  parseImportConfigDSL,
  prioritySequencePreview,
  serializeImportConfigDSL,
} from './importConfigDsl';

describe('importConfigDsl', () => {
  it('round trips fixed and bounded sequence rules', () => {
    const raw = JSON.stringify({
      version: 1,
      rules: [
        {
          name: 'plus',
          enabled: true,
          when: [{ field: 'credentials.plan_type', op: 'in', values: ['plus'] }],
          set: {
            max_concurrency: 20,
            priority: { mode: 'sequence', initial: 1000, step: -10, group_size: 5, min: 900, max: 1000 },
            group_ids: [2],
          },
        },
      ],
    });
    const parsed = parseImportConfigDSL(raw);
    expect(parseImportConfigDSL(serializeImportConfigDSL(parsed))).toEqual(parsed);
    const priority = parsed.rules[0]?.set.priority;
    expect(priority?.mode).toBe('sequence');
    if (priority?.mode === 'sequence') {
      expect(prioritySequencePreview(priority)).toEqual([1000, 990, 980, 970]);
    }
  });

  it('rejects invalid sequence bounds', () => {
    expect(() => parseImportConfigDSL(JSON.stringify({
      version: 1,
      rules: [{
        name: 'bad',
        when: [],
        set: { priority: { mode: 'sequence', initial: 100, step: -1, group_size: 1, min: 200, max: 100 } },
      }],
    }))).toThrow(/min\/max/);
  });
});
