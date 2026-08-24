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
            proxy_id: 7,
            proxy_slot: 'random',
            model_downgrade_threshold: 0.85,
          },
        },
      ],
    });
    const parsed = parseImportConfigDSL(raw);
    expect(parseImportConfigDSL(serializeImportConfigDSL(parsed))).toEqual(parsed);
    expect(parsed.rules[0]?.set).toEqual({
      max_concurrency: 20,
      priority: { mode: 'sequence', initial: 1000, step: -10, group_size: 5, min: 900, max: 1000 },
      group_ids: [2],
      proxy_id: 7,
      proxy_slot: 'random',
      model_downgrade_threshold: 0.85,
    });
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
        set: {
          priority: { mode: 'sequence', initial: 100, step: -1, group_size: 1, min: 200, max: 100 },
          model_downgrade_threshold: 0,
        },
      }],
    }))).toThrow(/min\/max/);
    for (const threshold of [undefined, null, '0.5']) {
      expect(() => parseImportConfigDSL(JSON.stringify({
        version: 1,
        rules: [{ name: 'bad threshold', when: [], set: { model_downgrade_threshold: threshold } }],
      }))).toThrow(/model_downgrade_threshold/);
    }
  });

  it('rejects removed assignment enabled fields', () => {
    for (const field of [
      'max_concurrency_enabled',
      'priority_enabled',
      'group_ids_enabled',
      'model_downgrade_threshold_enabled',
    ]) {
      expect(() => parseImportConfigDSL(JSON.stringify({
        version: 1,
        rules: [{
          name: 'legacy',
          when: [],
          set: { model_downgrade_threshold: 0, [field]: true },
        }],
      }))).toThrow(new RegExp(field));
    }
  });
});
