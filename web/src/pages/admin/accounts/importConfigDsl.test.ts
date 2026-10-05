import { describe, expect, it } from 'vitest';
import {
  createImportRule,
  parseImportConfigDSL,
  prioritySequencePreview,
  serializeImportConfigDSL,
} from './importConfigDsl';
import { ACCOUNT_IMPORT_DSL_EXAMPLE } from './ImportConfigModal';

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


describe('import scheduling weights', () => {
  it('includes the standard weight in new rules and example rules', () => {
    expect(createImportRule(1).set.scheduling_weight).toBe(100);
    const example = parseImportConfigDSL(ACCOUNT_IMPORT_DSL_EXAMPLE);
    expect(example.rules).toEqual([{
      name: 'OpenAI-Plus',
      enabled: true,
      when: [
        { field: 'platform', op: 'eq', value: 'openai' },
        { field: 'type', op: 'eq', value: 'oauth' },
        { field: 'credentials.plan_type', op: 'in', values: ['plus'] },
      ],
      set: {
        scheduling_weight: 100,
        max_concurrency: 15,
        priority: { mode: 'fixed', value: 5000 },
        group_ids: [],
        model_downgrade_threshold: 0,
      },
    }]);
  });
  const dsl = (weight: unknown) => JSON.stringify({ version: 1, rules: [{ name: 'weighted', when: [], set: { scheduling_weight: weight, model_downgrade_threshold: 0 } }] });
  it('preserves a configured weight when parsing and serializing', () => {
    const config = parseImportConfigDSL(dsl(0));
    expect(parseImportConfigDSL(serializeImportConfigDSL(config)).rules[0]?.set.scheduling_weight).toBe(0);
  });
  it('rejects negative, fractional, excessive and nonnumeric weights', () => {
    for (const weight of [-1, 1.5, 1000001, '100']) {
      expect(() => parseImportConfigDSL(dsl(weight))).toThrow(/scheduling_weight/);
    }
  });
});
