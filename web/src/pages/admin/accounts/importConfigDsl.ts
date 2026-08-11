export const IMPORT_PRIORITY_MIN = -99999;
export const IMPORT_PRIORITY_MAX = 99999;

export type ImportConditionOp = 'eq' | 'in' | 'contains' | 'prefix' | 'suffix' | 'empty' | 'not_empty';

export interface ImportCondition {
  field: string;
  op: ImportConditionOp;
  value?: string;
  values?: string[];
}

export type ImportPriority =
  | { mode: 'fixed'; value: number }
  | {
    mode: 'sequence';
    initial: number;
    step: number;
    group_size: number;
    min?: number;
    max?: number;
  };

export interface ImportAssignment {
  max_concurrency?: number;
  priority?: ImportPriority;
  group_ids?: number[];
  /** 模型降级阈值（0～1）：0 表示关闭，其它值表示开启。 */
  model_downgrade_threshold: number;
}

export interface ImportRule {
  name: string;
  enabled?: boolean;
  when: ImportCondition[];
  set: ImportAssignment;
}

export interface ImportConfigDSL {
  version: 1;
  rules: ImportRule[];
}

export const EMPTY_IMPORT_CONFIG: ImportConfigDSL = { version: 1, rules: [] };

const IMPORT_ASSIGNMENT_FIELDS = new Set([
  'max_concurrency',
  'priority',
  'group_ids',
  'model_downgrade_threshold',
]);

export function createImportRule(index: number): ImportRule {
  return {
    name: `Rule ${index}`,
    enabled: true,
    when: [{ field: 'type', op: 'eq', value: 'oauth' }],
    set: {
      max_concurrency: 10,
      priority: { mode: 'fixed', value: 50 },
      group_ids: [],
      model_downgrade_threshold: 0,
    },
  };
}

export function cloneImportConfig(config: ImportConfigDSL): ImportConfigDSL {
  return JSON.parse(JSON.stringify(config)) as ImportConfigDSL;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value != null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function isIntegerInRange(value: unknown, min: number, max: number): value is number {
  return Number.isSafeInteger(value) && Number(value) >= min && Number(value) <= max;
}

function parseCondition(value: unknown, ruleIndex: number, conditionIndex: number): ImportCondition {
  const source = asRecord(value);
  if (!source) throw new Error(`rules[${ruleIndex}].when[${conditionIndex}] must be an object`);
  const field = typeof source.field === 'string' ? source.field.trim() : '';
  const op = typeof source.op === 'string' ? source.op.trim() as ImportConditionOp : 'eq';
  if (!field) throw new Error(`rules[${ruleIndex}].when[${conditionIndex}].field is required`);
  if (!['eq', 'in', 'contains', 'prefix', 'suffix', 'empty', 'not_empty'].includes(op)) {
    throw new Error(`rules[${ruleIndex}].when[${conditionIndex}].op is invalid`);
  }
  if (op === 'in') {
    const values = Array.isArray(source.values)
      ? source.values.filter((item): item is string => typeof item === 'string').map((item) => item.trim()).filter(Boolean)
      : [];
    if (!values.length) throw new Error(`rules[${ruleIndex}].when[${conditionIndex}].values is required`);
    return { field, op, values };
  }
  if (op === 'empty' || op === 'not_empty') return { field, op };
  const conditionValue = typeof source.value === 'string' ? source.value.trim() : '';
  if (!conditionValue) throw new Error(`rules[${ruleIndex}].when[${conditionIndex}].value is required`);
  return { field, op, value: conditionValue };
}

function parsePriority(value: unknown, ruleIndex: number): ImportPriority {
  const source = asRecord(value);
  if (!source) throw new Error(`rules[${ruleIndex}].set.priority must be an object`);
  if (source.mode === 'fixed') {
    if (!isIntegerInRange(source.value, IMPORT_PRIORITY_MIN, IMPORT_PRIORITY_MAX)) {
      throw new Error(`rules[${ruleIndex}].set.priority.value is invalid`);
    }
    return { mode: 'fixed', value: source.value };
  }
  if (source.mode !== 'sequence') throw new Error(`rules[${ruleIndex}].set.priority.mode is invalid`);
  const minimum = source.min == null ? IMPORT_PRIORITY_MIN : source.min;
  const maximum = source.max == null ? IMPORT_PRIORITY_MAX : source.max;
  if (!isIntegerInRange(minimum, IMPORT_PRIORITY_MIN, IMPORT_PRIORITY_MAX)
    || !isIntegerInRange(maximum, IMPORT_PRIORITY_MIN, IMPORT_PRIORITY_MAX)
    || minimum > maximum) {
    throw new Error(`rules[${ruleIndex}].set.priority min/max is invalid`);
  }
  if (!isIntegerInRange(source.initial, minimum, maximum)) {
    throw new Error(`rules[${ruleIndex}].set.priority.initial is invalid`);
  }
  const step = Number(source.step);
  if (!Number.isSafeInteger(step) || step === 0) {
    throw new Error(`rules[${ruleIndex}].set.priority.step is invalid`);
  }
  const groupSize = Number(source.group_size);
  if (!Number.isSafeInteger(groupSize) || groupSize <= 0) {
    throw new Error(`rules[${ruleIndex}].set.priority.group_size is invalid`);
  }
  return {
    mode: 'sequence',
    initial: source.initial,
    step,
    group_size: groupSize,
    ...(source.min == null ? {} : { min: minimum }),
    ...(source.max == null ? {} : { max: maximum }),
  };
}

function parseRule(value: unknown, ruleIndex: number): ImportRule {
  const source = asRecord(value);
  if (!source) throw new Error(`rules[${ruleIndex}] must be an object`);
  const name = typeof source.name === 'string' ? source.name.trim() : '';
  if (!name) throw new Error(`rules[${ruleIndex}].name is required`);
  if (!Array.isArray(source.when)) throw new Error(`rules[${ruleIndex}].when must be an array`);
  const set = asRecord(source.set);
  if (!set) throw new Error(`rules[${ruleIndex}].set must be an object`);
  for (const field of Object.keys(set)) {
    if (!IMPORT_ASSIGNMENT_FIELDS.has(field)) {
      throw new Error(`rules[${ruleIndex}].set.${field} is unknown`);
    }
  }
  const assignment = {} as ImportAssignment;
  if (set.max_concurrency != null) {
    if (!Number.isSafeInteger(set.max_concurrency) || Number(set.max_concurrency) < 0) {
      throw new Error(`rules[${ruleIndex}].set.max_concurrency is invalid`);
    }
    assignment.max_concurrency = Number(set.max_concurrency);
  }
  if (set.priority != null) assignment.priority = parsePriority(set.priority, ruleIndex);
  if (set.group_ids != null) {
    if (!Array.isArray(set.group_ids)
      || set.group_ids.some((id) => !Number.isSafeInteger(id) || Number(id) <= 0)) {
      throw new Error(`rules[${ruleIndex}].set.group_ids is invalid`);
    }
    const groupIDs = set.group_ids.map(Number);
    if (new Set(groupIDs).size !== groupIDs.length) {
      throw new Error(`rules[${ruleIndex}].set.group_ids contains duplicates`);
    }
    assignment.group_ids = groupIDs;
  }
  const hasModelDowngradeThreshold = Object.prototype.hasOwnProperty.call(set, 'model_downgrade_threshold');
  const threshold = set.model_downgrade_threshold;
  if (!hasModelDowngradeThreshold
    || typeof threshold !== 'number'
    || !Number.isFinite(threshold)
    || threshold < 0
    || threshold > 1) {
    throw new Error(`rules[${ruleIndex}].set.model_downgrade_threshold is invalid`);
  }
  assignment.model_downgrade_threshold = threshold;
  return {
    name,
    ...(typeof source.enabled === 'boolean' ? { enabled: source.enabled } : {}),
    when: source.when.map((condition, conditionIndex) => parseCondition(condition, ruleIndex, conditionIndex)),
    set: assignment,
  };
}

export function parseImportConfigDSL(raw: string): ImportConfigDSL {
  const source = asRecord(JSON.parse(raw));
  if (!source || source.version !== 1 || !Array.isArray(source.rules)) {
    throw new Error('version=1 and rules are required');
  }
  return { version: 1, rules: source.rules.map(parseRule) };
}

export function serializeImportConfigDSL(config: ImportConfigDSL): string {
  return JSON.stringify(config, null, 2);
}

export function prioritySequencePreview(priority: Extract<ImportPriority, { mode: 'sequence' }>, levels = 4): number[] {
  return Array.from({ length: levels }, (_, index) => priority.initial + index * priority.step);
}
