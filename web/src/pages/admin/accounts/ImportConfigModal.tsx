import { useCallback, useEffect, useMemo, useState, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button, ComboBox, Input, Label, ListBox, Spinner, Tabs, TextArea, TextField as HeroTextField, useOverlayState,
} from '@heroui/react';
import {
  Braces, CopyPlus, GripVertical, Plus, RotateCcw, Trash2,
} from 'lucide-react';
import { CommonModal } from '../../../shared/components/CommonModal';
import { NativeCheckbox } from '../../../shared/components/NativeCheckbox';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import { ToolbarMenuItem } from '../../../shared/components/ToolbarMenu';
import type { GroupResp } from '../../../shared/types';
import {
  EMPTY_IMPORT_CONFIG,
  IMPORT_PRIORITY_MAX,
  IMPORT_PRIORITY_MIN,
  cloneImportConfig,
  createImportRule,
  parseImportConfigDSL,
  serializeImportConfigDSL,
  type ImportCondition,
  type ImportConditionOp,
  type ImportConfigDSL,
  type ImportPriority,
  type ImportRule,
} from './importConfigDsl';

const CONDITION_FIELD_SUGGESTIONS = [
  'platform',
  'type',
  'name',
  'email',
  'credentials.plan_type',
  'credentials.provider',
  'extra.subscription_type',
] as const;

const CONDITION_OPERATORS: Array<{ key: ImportConditionOp; labelKey: string }> = [
  { key: 'eq', labelKey: 'accounts.import_config_op_eq' },
  { key: 'in', labelKey: 'accounts.import_config_op_in' },
  { key: 'contains', labelKey: 'accounts.import_config_op_contains' },
  { key: 'prefix', labelKey: 'accounts.import_config_op_prefix' },
  { key: 'suffix', labelKey: 'accounts.import_config_op_suffix' },
  { key: 'empty', labelKey: 'accounts.import_config_op_empty' },
  { key: 'not_empty', labelKey: 'accounts.import_config_op_not_empty' },
];

export const ACCOUNT_IMPORT_DSL_EXAMPLE = serializeImportConfigDSL({
  version: 1,
  rules: [
    examplePlanRule('OpenAI OAuth Free', ['free'], 5, { mode: 'fixed', value: 50 }),
    examplePlanRule('OpenAI OAuth Plus', ['plus'], 20, {
      mode: 'sequence', initial: 1000, step: -10, group_size: 5, min: 800, max: 1000,
    }),
    examplePlanRule('OpenAI OAuth Pro', ['pro'], 30, { mode: 'fixed', value: 300 }),
    examplePlanRule('OpenAI OAuth Team', ['team', 'k12'], 50, {
      mode: 'sequence', initial: 3000, step: -20, group_size: 10, min: 2600, max: 3000,
    }),
  ],
});

function examplePlanRule(
  name: string,
  planTypes: string[],
  maxConcurrency: number,
  priority: ImportPriority,
): ImportRule {
  return {
    name,
    enabled: true,
    when: [
      { field: 'platform', op: 'eq', value: 'openai' },
      { field: 'type', op: 'eq', value: 'oauth' },
      { field: 'credentials.plan_type', op: 'in', values: planTypes },
    ],
    set: { max_concurrency: maxConcurrency, priority, group_ids: [] },
  };
}

function conditionDisplayValue(condition: ImportCondition): string {
  return condition.op === 'in' ? (condition.values ?? []).join(', ') : condition.value ?? '';
}

function ruleSummary(rule: ImportRule): string {
  const plan = rule.when.find((condition) => condition.field === 'credentials.plan_type');
  if (plan) return conditionDisplayValue(plan) || 'plan_type';
  const type = rule.when.find((condition) => condition.field === 'type' || condition.field === 'account_type');
  if (type) return conditionDisplayValue(type) || type.field;
  return rule.when.length === 0 ? '*' : `${rule.when.length} conditions`;
}

function parseNumber(value: string, fallback: number): number {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : fallback;
}

function validateConfig(config: ImportConfigDSL): string {
  try {
    parseImportConfigDSL(serializeImportConfigDSL(config));
    return '';
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
}

export function ImportConfigModal({
  open,
  dsl,
  groups,
  loading,
  onClose,
  onSubmit,
}: {
  open: boolean;
  dsl: string;
  groups: GroupResp[];
  loading: boolean;
  onClose: () => void;
  onSubmit: (dsl: string) => void;
}) {
  const { t } = useTranslation();
  const [view, setView] = useState<'form' | 'dsl'>('form');
  const [config, setConfig] = useState<ImportConfigDSL>(() => cloneImportConfig(EMPTY_IMPORT_CONFIG));
  const [selectedRuleIndex, setSelectedRuleIndex] = useState(0);
  const [dslValue, setDSLValue] = useState(() => serializeImportConfigDSL(EMPTY_IMPORT_CONFIG));
  const [dslError, setDSLError] = useState('');
  const [draggingIndex, setDraggingIndex] = useState<number | null>(null);
  const [dropIndicator, setDropIndicator] = useState<{ index: number; position: 'before' | 'after' } | null>(null);
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen && !loading) onClose();
    },
  });

  useEffect(() => {
    const raw = dsl.trim() || serializeImportConfigDSL(EMPTY_IMPORT_CONFIG);
    setDSLValue(raw);
    setDSLError('');
    setView('form');
    try {
      const parsed = parseImportConfigDSL(raw);
      setConfig(parsed);
      setSelectedRuleIndex(0);
    } catch (error) {
      setConfig(cloneImportConfig(EMPTY_IMPORT_CONFIG));
      setSelectedRuleIndex(0);
      setView('dsl');
      setDSLError(error instanceof Error ? error.message : String(error));
    }
  }, [dsl, open]);

  const sortedGroups = useMemo(
    () => [...groups].sort((left, right) => (
      left.platform.localeCompare(right.platform) || left.name.localeCompare(right.name)
    )),
    [groups],
  );
  const selectedRule = config.rules[selectedRuleIndex];
  const displayedPriority: ImportPriority = selectedRule?.set.priority ?? { mode: 'fixed', value: 50 };
  const capacityEnabled = selectedRule?.set.max_concurrency != null
    && selectedRule.set.max_concurrency_enabled !== false;
  const priorityEnabled = selectedRule?.set.priority != null
    && selectedRule.set.priority_enabled !== false;
  const groupsEnabled = selectedRule?.set.group_ids != null
    && selectedRule.set.group_ids_enabled !== false;
  const validationError = useMemo(() => validateConfig(config), [config]);
  const selectedPlatform = selectedRule?.when.find(
    (condition) => condition.field === 'platform' && condition.op === 'eq',
  )?.value?.trim().toLowerCase();
  const visibleGroups = useMemo(
    () => selectedPlatform
      ? sortedGroups.filter((group) => group.platform.toLowerCase() === selectedPlatform)
      : sortedGroups,
    [selectedPlatform, sortedGroups],
  );

  const updateSelectedRule = useCallback((updater: (rule: ImportRule) => void) => {
    setConfig((current) => {
      if (!current.rules[selectedRuleIndex]) return current;
      const next = cloneImportConfig(current);
      updater(next.rules[selectedRuleIndex]!);
      return next;
    });
  }, [selectedRuleIndex]);

  const switchView = (nextView: 'form' | 'dsl') => {
    if (nextView === view) return;
    if (nextView === 'dsl') {
      setDSLValue(serializeImportConfigDSL(config));
      setDSLError(validationError);
      setView('dsl');
      return;
    }
    try {
      const parsed = parseImportConfigDSL(dslValue);
      setConfig(parsed);
      setSelectedRuleIndex(Math.min(selectedRuleIndex, Math.max(0, parsed.rules.length - 1)));
      setDSLError('');
      setView('form');
    } catch (error) {
      setDSLError(error instanceof Error ? error.message : String(error));
    }
  };

  const loadConfig = (raw: string) => {
    const parsed = parseImportConfigDSL(raw);
    setConfig(parsed);
    setDSLValue(serializeImportConfigDSL(parsed));
    setSelectedRuleIndex(0);
    setDSLError('');
    setView('form');
  };

  const handleSave = () => {
    if (view === 'dsl') {
      try {
        const parsed = parseImportConfigDSL(dslValue);
        const serialized = serializeImportConfigDSL(parsed);
        setDSLError('');
        onSubmit(serialized);
      } catch (error) {
        setDSLError(error instanceof Error ? error.message : String(error));
      }
      return;
    }
    if (validationError) return;
    onSubmit(serializeImportConfigDSL(config));
  };

  const addRule = () => {
    setConfig((current) => {
      const next = cloneImportConfig(current);
      next.rules.push(createImportRule(next.rules.length + 1));
      return next;
    });
    setSelectedRuleIndex(config.rules.length);
  };

  const duplicateRule = () => {
    if (!selectedRule) return;
    setConfig((current) => {
      const next = cloneImportConfig(current);
      const duplicated = cloneImportConfig({ version: 1, rules: [next.rules[selectedRuleIndex]!] }).rules[0]!;
      duplicated.name = `${duplicated.name} Copy`;
      next.rules.splice(selectedRuleIndex + 1, 0, duplicated);
      return next;
    });
    setSelectedRuleIndex(selectedRuleIndex + 1);
  };

  const deleteRule = () => {
    if (!selectedRule) return;
    setConfig((current) => {
      const next = cloneImportConfig(current);
      next.rules.splice(selectedRuleIndex, 1);
      return next;
    });
    setSelectedRuleIndex(Math.max(0, selectedRuleIndex - 1));
  };

  const reorderRule = (from: number, insertBefore: number) => {
    if (from === insertBefore || from + 1 === insertBefore) return;
    const target = insertBefore > from ? insertBefore - 1 : insertBefore;
    setConfig((current) => {
      if (!current.rules[from]) return current;
      const next = cloneImportConfig(current);
      const [moved] = next.rules.splice(from, 1);
      if (!moved) return current;
      next.rules.splice(target, 0, moved);
      return next;
    });
    setSelectedRuleIndex(target);
  };

  const resetRuleDrag = () => {
    setDraggingIndex(null);
    setDropIndicator(null);
  };

  const handleRuleDragStart = (index: number) => (event: DragEvent<HTMLDivElement>) => {
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(index));
    setDraggingIndex(index);
  };

  const handleRuleDragOver = (index: number) => (event: DragEvent<HTMLDivElement>) => {
    if (draggingIndex === null) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
    const rect = event.currentTarget.getBoundingClientRect();
    const position = event.clientY < rect.top + rect.height / 2 ? 'before' : 'after';
    setDropIndicator((current) => (
      current && current.index === index && current.position === position
        ? current
        : { index, position }
    ));
  };

  const handleRuleDrop = (index: number) => (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    if (draggingIndex !== null) {
      const rect = event.currentTarget.getBoundingClientRect();
      const position = event.clientY < rect.top + rect.height / 2 ? 'before' : 'after';
      reorderRule(draggingIndex, position === 'before' ? index : index + 1);
    }
    resetRuleDrag();
  };

  const updateCondition = (conditionIndex: number, updater: (condition: ImportCondition) => ImportCondition) => {
    updateSelectedRule((rule) => {
      const condition = rule.when[conditionIndex];
      if (condition) rule.when[conditionIndex] = updater(condition);
    });
  };

  const setConditionOperator = (conditionIndex: number, op: ImportConditionOp) => {
    updateCondition(conditionIndex, (condition) => {
      if (op === 'in') {
        return {
          field: condition.field,
          op,
          values: condition.values?.length ? condition.values : condition.value ? [condition.value] : [],
        };
      }
      if (op === 'empty' || op === 'not_empty') return { field: condition.field, op };
      return {
        field: condition.field,
        op,
        value: condition.value ?? condition.values?.[0] ?? '',
      };
    });
  };

  const setPriorityMode = (mode: 'fixed' | 'sequence') => {
    updateSelectedRule((rule) => {
      const current = rule.set.priority;
      if (mode === 'fixed') {
        rule.set.priority = {
          mode: 'fixed',
          value: current?.mode === 'fixed' ? current.value : current?.initial ?? 50,
        };
        return;
      }
      rule.set.priority = {
        mode: 'sequence',
        initial: current?.mode === 'sequence' ? current.initial : current?.value ?? 1000,
        step: current?.mode === 'sequence' ? current.step : -1,
        group_size: current?.mode === 'sequence' ? current.group_size : 5,
        min: current?.mode === 'sequence' ? current.min ?? IMPORT_PRIORITY_MIN : IMPORT_PRIORITY_MIN,
        max: current?.mode === 'sequence' ? current.max ?? IMPORT_PRIORITY_MAX : IMPORT_PRIORITY_MAX,
      };
    });
  };

  const setSequenceValue = (
    key: 'initial' | 'step' | 'group_size' | 'min' | 'max',
    value: number,
  ) => {
    updateSelectedRule((rule) => {
      if (rule.set.priority?.mode === 'sequence') rule.set.priority[key] = value;
    });
  };

  const canSave = !loading && (view === 'dsl' ? dslError === '' : validationError === '');

  return (
    <CommonModal
      className="ag-account-page-modal ag-import-config-modal"
      description={t('accounts.import_config_description')}
      dialogStyle={{
        height: 'min(820px, calc(100dvh - 2rem))',
        maxWidth: '980px',
        width: 'min(100%, calc(100vw - 2rem))',
      }}
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose} isDisabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="primary"
            onPress={handleSave}
            isDisabled={!canSave}
            aria-busy={loading}
          >
            {loading ? <Spinner size="sm" /> : null}
            {t('common.save')}
          </Button>
        </div>
      )}
      size="lg"
      state={modalState}
      surface={false}
      title={t('accounts.import_config_title')}
    >
      <div className="ag-import-config">
        <div className="ag-import-config-toolbar">
          <Tabs
            className="ag-segmented-tabs ag-segmented-tabs-compact ag-segmented-tabs-auto"
            isDisabled={loading}
            selectedKey={view}
            onSelectionChange={(key) => switchView(key as 'form' | 'dsl')}
          >
            <Tabs.List>
              <Tabs.Tab id="form">
                <Tabs.Indicator />
                <span>{t('accounts.import_config_graphical')}</span>
              </Tabs.Tab>
              <Tabs.Tab id="dsl">
                <Tabs.Separator />
                <Tabs.Indicator />
                <Braces />
                <span>{t('accounts.import_config_advanced')}</span>
              </Tabs.Tab>
            </Tabs.List>
          </Tabs>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onPress={() => loadConfig(ACCOUNT_IMPORT_DSL_EXAMPLE)} isDisabled={loading}>
              {t('accounts.import_config_load_example')}
            </Button>
            <Button
              variant="ghost"
              onPress={() => loadConfig(serializeImportConfigDSL(EMPTY_IMPORT_CONFIG))}
              isDisabled={loading}
            >
              <RotateCcw className="h-4 w-4" />
              {t('accounts.import_config_clear')}
            </Button>
          </div>
        </div>

        {view === 'dsl' ? (
          <div className="ag-import-config-dsl">
            <HeroTextField fullWidth isInvalid={dslError !== ''}>
              <Label>{t('accounts.import_config_dsl')}</Label>
              <TextArea
                className="ag-import-config-dsl-input font-mono text-xs leading-5"
                wrap="off"
                value={dslValue}
                disabled={loading}
                onChange={(event) => {
                  setDSLValue(event.target.value);
                  try {
                    parseImportConfigDSL(event.target.value);
                    setDSLError('');
                  } catch (error) {
                    setDSLError(error instanceof Error ? error.message : String(error));
                  }
                }}
              />
            </HeroTextField>
            {dslError ? <p className="text-sm text-danger">{dslError}</p> : null}
          </div>
        ) : (
          <div className="ag-import-config-body">
            <aside className="ag-import-config-sidebar">
              <div className="ag-import-config-sidebar-header">
                <span className="text-xs font-medium text-text-secondary">{t('accounts.import_config_rules')}</span>
                <span className="text-[11px] text-text-tertiary">{t('accounts.import_config_first_match')}</span>
              </div>
              <div className="ag-import-config-rule-list ag-simple-multi-select">
                {config.rules.map((rule, index) => (
                  <div
                    key={`${rule.name}-${index}`}
                    className="ag-import-config-rule"
                    data-dragging={draggingIndex === index ? 'true' : undefined}
                    data-drop-position={
                      dropIndicator?.index === index ? dropIndicator.position : undefined
                    }
                    draggable
                    onDragEnd={resetRuleDrag}
                    onDragOver={handleRuleDragOver(index)}
                    onDragStart={handleRuleDragStart(index)}
                    onDrop={handleRuleDrop(index)}
                  >
                    <span className="ag-import-config-rule-grip" aria-hidden="true">
                      <GripVertical className="h-3.5 w-3.5" />
                    </span>
                    <ToolbarMenuItem
                      isSelected={index === selectedRuleIndex}
                      role="menuitemradio"
                      showCheckIndicator={false}
                      onSelect={() => setSelectedRuleIndex(index)}
                    >
                      <span className="flex min-w-0 items-center justify-between gap-2">
                        <span className="min-w-0">
                          <span className="block truncate text-sm">{rule.name}</span>
                          <span className="block truncate text-[11px] opacity-70">{ruleSummary(rule)}</span>
                        </span>
                        <span className="text-[11px] tabular-nums opacity-70">{index + 1}</span>
                      </span>
                    </ToolbarMenuItem>
                  </div>
                ))}
                {config.rules.length === 0 ? (
                  <p className="px-2 py-6 text-center text-xs text-text-tertiary">
                    {t('accounts.import_config_no_rules')}
                  </p>
                ) : null}
              </div>
              <div className="ag-import-config-sidebar-footer">
                <Button className="w-full" variant="secondary" onPress={addRule} isDisabled={loading}>
                  <Plus className="h-4 w-4" />
                  {t('accounts.import_config_add_rule')}
                </Button>
              </div>
            </aside>

            <div className="ag-import-config-editor">
              <div className="space-y-4 pb-1">
              {selectedRule ? (
                <>
                  <div className="flex flex-wrap items-end justify-between gap-x-3 gap-y-2">
                    <div className="min-w-[220px] flex-1">
                      <HeroTextField fullWidth>
                        <Label>{t('accounts.import_config_rule_name')}</Label>
                        <Input
                          value={selectedRule.name}
                          onChange={(event) => updateSelectedRule((rule) => { rule.name = event.target.value; })}
                        />
                      </HeroTextField>
                    </div>
                    <div className="flex flex-wrap items-center gap-1 pb-0.5">
                      <NativeSwitch
                        isSelected={selectedRule.enabled !== false}
                        label={t('accounts.import_config_rule_enabled')}
                        onChange={(enabled) => updateSelectedRule((rule) => { rule.enabled = enabled; })}
                      />
                      <Button isIconOnly variant="ghost" aria-label={t('accounts.import_config_duplicate_rule')} onPress={duplicateRule}>
                        <CopyPlus className="h-4 w-4" />
                      </Button>
                      <Button isIconOnly variant="ghost" aria-label={t('accounts.import_config_delete_rule')} onPress={deleteRule}>
                        <Trash2 className="h-4 w-4 text-danger" />
                      </Button>
                    </div>
                  </div>

                  <section className="space-y-2 border-t border-border pt-4">
                    <div className="flex items-center justify-between gap-2">
                      <div>
                        <h3 className="text-sm font-semibold text-text">{t('accounts.import_config_conditions')}</h3>
                        <p className="text-xs text-text-tertiary">{t('accounts.import_config_conditions_hint')}</p>
                      </div>
                      <Button
                        variant="secondary"
                        onPress={() => updateSelectedRule((rule) => {
                          rule.when.push({ field: 'credentials.plan_type', op: 'eq', value: '' });
                        })}
                      >
                        <Plus className="h-4 w-4" />
                        {t('accounts.import_config_add_condition')}
                      </Button>
                    </div>
                    {selectedRule.when.map((condition, conditionIndex) => {
                      const operator = CONDITION_OPERATORS.find((item) => item.key === condition.op);
                      return (
                        <div key={conditionIndex} className="ag-import-config-condition grid items-center gap-2 rounded-md bg-surface px-2.5 py-2 sm:grid-cols-[1.2fr_0.8fr_1.4fr_auto]">
                          <ComboBox
                            fullWidth
                            allowsCustomValue
                            aria-label={t('accounts.import_config_field')}
                            inputValue={condition.field}
                            onInputChange={(value) => updateCondition(conditionIndex, (current) => ({
                              ...current, field: value,
                            }))}
                            onSelectionChange={(key) => {
                              if (key == null) return;
                              updateCondition(conditionIndex, (current) => ({
                                ...current, field: String(key),
                              }));
                            }}
                          >
                            <ComboBox.InputGroup>
                              <Input placeholder={t('accounts.import_config_field')} />
                              <ComboBox.Trigger />
                            </ComboBox.InputGroup>
                            <ComboBox.Popover className="ag-import-config-field-popover">
                              <ListBox>
                                {CONDITION_FIELD_SUGGESTIONS.map((field) => (
                                  <ListBox.Item key={field} id={field} textValue={field}>
                                    {field}
                                  </ListBox.Item>
                                ))}
                              </ListBox>
                            </ComboBox.Popover>
                          </ComboBox>
                          <SimpleSelect
                            ariaLabel={t('accounts.import_config_operator')}
                            fullWidth
                            items={CONDITION_OPERATORS.map((item) => ({ key: item.key, label: t(item.labelKey) }))}
                            selectedKey={condition.op}
                            selectedLabel={operator ? t(operator.labelKey) : condition.op}
                            onSelectionChange={(key) => setConditionOperator(conditionIndex, key as ImportConditionOp)}
                          />
                          {condition.op === 'empty' || condition.op === 'not_empty' ? <div /> : (
                            <Input
                              aria-label={t('accounts.import_config_value')}
                              className="w-full"
                              placeholder={condition.op === 'in'
                                ? t('accounts.import_config_values_placeholder')
                                : t('accounts.import_config_value')}
                              value={conditionDisplayValue(condition)}
                              onChange={(event) => updateCondition(conditionIndex, (current) => (
                                current.op === 'in'
                                  ? { ...current, values: event.target.value.split(',').map((item) => item.trim()).filter(Boolean) }
                                  : { ...current, value: event.target.value }
                              ))}
                            />
                          )}
                          <Button
                            isIconOnly
                            variant="ghost"
                            aria-label={t('accounts.import_config_delete_condition')}
                            onPress={() => updateSelectedRule((rule) => { rule.when.splice(conditionIndex, 1); })}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      );
                    })}
                  </section>

                  <section className="space-y-4 border-t border-border pt-4">
                    <h3 className="text-sm font-semibold text-text">{t('accounts.import_config_assignments')}</h3>

                    <div className="grid gap-4 rounded-md bg-surface px-3 py-3 sm:grid-cols-2">
                      <div className="flex flex-col items-start gap-2">
                        <NativeSwitch
                          isSelected={capacityEnabled}
                          label={t('accounts.import_config_capacity')}
                          onChange={(enabled) => updateSelectedRule((rule) => {
                            rule.set.max_concurrency = rule.set.max_concurrency ?? 10;
                            rule.set.max_concurrency_enabled = enabled;
                          })}
                        />
                        <Input
                          aria-label={t('accounts.import_config_capacity')}
                          className="w-full"
                          type="number"
                          min={0}
                          disabled={!capacityEnabled}
                          value={String(selectedRule.set.max_concurrency ?? 10)}
                          onChange={(event) => updateSelectedRule((rule) => {
                            rule.set.max_concurrency = Math.max(0, parseNumber(event.target.value, 0));
                          })}
                        />
                      </div>
                      <div className="flex flex-col items-start gap-2">
                        <NativeSwitch
                          isSelected={priorityEnabled}
                          label={t('accounts.import_config_priority')}
                          onChange={(enabled) => updateSelectedRule((rule) => {
                            rule.set.priority = rule.set.priority ?? { mode: 'fixed', value: 50 };
                            rule.set.priority_enabled = enabled;
                          })}
                        />
                        <SimpleSelect
                          ariaLabel={t('accounts.import_config_priority_mode')}
                          fullWidth
                          isDisabled={!priorityEnabled}
                          items={[
                            { key: 'fixed', label: t('accounts.import_config_priority_fixed') },
                            { key: 'sequence', label: t('accounts.priority_sequence') },
                          ]}
                          selectedKey={displayedPriority.mode}
                          onSelectionChange={(key) => setPriorityMode(key as 'fixed' | 'sequence')}
                        />
                      </div>
                      {displayedPriority.mode === 'fixed' ? (
                        <div className="grid gap-2 border-t border-border pt-3 sm:col-span-2 sm:grid-cols-3">
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.priority')}</Label>
                            <Input
                              type="number"
                              min={IMPORT_PRIORITY_MIN}
                              max={IMPORT_PRIORITY_MAX}
                              disabled={!priorityEnabled}
                              value={String(displayedPriority.value)}
                              onChange={(event) => updateSelectedRule((rule) => {
                                if (rule.set.priority?.mode === 'fixed') {
                                  rule.set.priority.value = parseNumber(event.target.value, 0);
                                }
                              })}
                            />
                          </HeroTextField>
                        </div>
                      ) : null}
                      {displayedPriority.mode === 'sequence' ? (
                        <div className="grid gap-2 border-t border-border pt-3 sm:col-span-2 sm:grid-cols-5">
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.priority_sequence_initial')}</Label>
                            <Input disabled={!priorityEnabled} type="number" value={String(displayedPriority.initial)} onChange={(event) => setSequenceValue('initial', parseNumber(event.target.value, 0))} />
                          </HeroTextField>
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.priority_sequence_step')}</Label>
                            <Input disabled={!priorityEnabled} type="number" value={String(displayedPriority.step)} onChange={(event) => setSequenceValue('step', parseNumber(event.target.value, 0))} />
                          </HeroTextField>
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.priority_sequence_group_size')}</Label>
                            <Input disabled={!priorityEnabled} type="number" min={1} value={String(displayedPriority.group_size)} onChange={(event) => setSequenceValue('group_size', Math.max(1, parseNumber(event.target.value, 1)))} />
                          </HeroTextField>
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.import_config_priority_min')}</Label>
                            <Input disabled={!priorityEnabled} type="number" min={IMPORT_PRIORITY_MIN} max={IMPORT_PRIORITY_MAX} value={String(displayedPriority.min ?? IMPORT_PRIORITY_MIN)} onChange={(event) => setSequenceValue('min', parseNumber(event.target.value, IMPORT_PRIORITY_MIN))} />
                          </HeroTextField>
                          <HeroTextField fullWidth isDisabled={!priorityEnabled}>
                            <Label>{t('accounts.import_config_priority_max')}</Label>
                            <Input disabled={!priorityEnabled} type="number" min={IMPORT_PRIORITY_MIN} max={IMPORT_PRIORITY_MAX} value={String(displayedPriority.max ?? IMPORT_PRIORITY_MAX)} onChange={(event) => setSequenceValue('max', parseNumber(event.target.value, IMPORT_PRIORITY_MAX))} />
                          </HeroTextField>
                        </div>
                      ) : null}
                    </div>

                    <div className="space-y-3 rounded-md bg-surface px-3 py-3">
                      <NativeSwitch
                        isSelected={groupsEnabled}
                        label={t('accounts.import_config_groups_assignment')}
                        onChange={(enabled) => updateSelectedRule((rule) => {
                          rule.set.group_ids = rule.set.group_ids ?? [];
                          rule.set.group_ids_enabled = enabled;
                        })}
                      />
                      <div className="grid max-h-44 gap-2 overflow-y-auto sm:grid-cols-2">
                        {visibleGroups.map((group) => (
                          <NativeCheckbox
                            key={group.id}
                            isDisabled={!groupsEnabled}
                            isSelected={selectedRule.set.group_ids?.includes(group.id) ?? false}
                            onChange={(selected) => updateSelectedRule((rule) => {
                              const ids = rule.set.group_ids ?? [];
                              rule.set.group_ids = selected
                                ? Array.from(new Set([...ids, group.id]))
                                : ids.filter((id) => id !== group.id);
                            })}
                          >
                            <span className={!groupsEnabled ? 'text-sm text-text-tertiary' : 'text-sm text-text'}>
                              {group.name}
                              <span className="ml-1 text-xs text-text-tertiary">#{group.id} · {group.platform}</span>
                            </span>
                          </NativeCheckbox>
                        ))}
                        {visibleGroups.length === 0 ? (
                          <p className="text-xs text-text-tertiary">{t('accounts.import_config_no_groups')}</p>
                        ) : null}
                      </div>
                    </div>
                  </section>

                  {validationError ? (
                    <p className="rounded-md bg-danger/10 px-3 py-2 text-xs text-danger">{validationError}</p>
                  ) : null}
                </>
              ) : (
                <div className="flex h-full min-h-64 flex-col items-center justify-center gap-3 text-text-tertiary">
                  <p className="text-sm">{t('accounts.import_config_no_rules')}</p>
                  <Button variant="secondary" onPress={addRule}>
                    <Plus className="h-4 w-4" />
                    {t('accounts.import_config_add_rule')}
                  </Button>
                </div>
              )}
              </div>
            </div>
          </div>
        )}
      </div>
    </CommonModal>
  );
}
