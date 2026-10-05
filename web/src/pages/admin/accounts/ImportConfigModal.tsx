import styles from './ImportConfigModal.module.css';
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
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import { DropdownIndicator } from '../../../shared/components/DropdownIndicator';
import { ToolbarMenuItem } from '../../../shared/components/ToolbarMenu';
import type { GroupResp, ProxyResp } from '../../../shared/types';
import { ProxyBindingFields, resolveProxyBinding } from './ProxyBindingFields';
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
import {
  DEFAULT_SCHEDULING_WEIGHT,
  MAX_SCHEDULING_WEIGHT,
  parseSchedulingWeightInput,
  parseModelDowngradeThresholdInput
} from './accountDefaults';

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
  rules: [{
    name: 'OpenAI-Plus',
    enabled: true,
    when: [
      { field: 'platform', op: 'eq', value: 'openai' },
      { field: 'type', op: 'eq', value: 'oauth' },
      { field: 'credentials.plan_type', op: 'in', values: ['plus'] },
    ],
    set: {
      scheduling_weight: DEFAULT_SCHEDULING_WEIGHT,
      max_concurrency: 15,
      priority: { mode: 'fixed', value: 5000 },
      group_ids: [],
      model_downgrade_threshold: 0,
    },
  }],
});

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
  proxies,
  loading,
  onClose,
  onSubmit,
}: {
  open: boolean;
  dsl: string;
  groups: GroupResp[];
  proxies: ProxyResp[];
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
  const [schedulingWeightInput, setSchedulingWeightInput] = useState('');
  const schedulingWeightValid = schedulingWeightInput.trim() === '' || parseSchedulingWeightInput(schedulingWeightInput) != null;
  const [modelDowngradeThresholdInput, setModelDowngradeThresholdInput] = useState('0');
  const [proxySlotInput, setProxySlotInput] = useState('');
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
  const modelDowngradeThresholdValue = parseModelDowngradeThresholdInput(modelDowngradeThresholdInput);
  const modelDowngradeThresholdValid = modelDowngradeThresholdValue != null;
  const proxyID = selectedRule?.set.proxy_id ?? null;
  const proxyBinding = resolveProxyBinding(proxies, proxyID, proxySlotInput);
  const configValidationError = useMemo(() => validateConfig(config), [config]);
  const proxyAssignmentsValid = useMemo(() => config.rules.every((rule) => resolveProxyBinding(
    proxies,
    rule.set.proxy_id,
    rule.set.proxy_slot == null ? '' : String(rule.set.proxy_slot),
  ).valid), [config.rules, proxies]);
  const validationError = !schedulingWeightValid ? t('accounts.scheduling_weight_invalid') : !modelDowngradeThresholdValid
    ? t('accounts.model_downgrade_threshold_invalid')
    : !proxyBinding.valid || !proxyAssignmentsValid
      ? t('accounts.proxy_slot_invalid')
      : configValidationError;
  const selectedPlatform = selectedRule?.when.find(
    (condition) => condition.field === 'platform' && condition.op === 'eq',
  )?.value?.trim().toLowerCase();
  const visibleGroups = useMemo(
    () => selectedPlatform
      ? sortedGroups.filter((group) => group.platform.toLowerCase() === selectedPlatform)
      : sortedGroups,
    [selectedPlatform, sortedGroups],
  );

  useEffect(() => {
    setSchedulingWeightInput(selectedRule?.set.scheduling_weight == null ? '' : String(selectedRule.set.scheduling_weight));
  }, [selectedRuleIndex, selectedRule?.set.scheduling_weight]);

  useEffect(() => {
    setModelDowngradeThresholdInput(
      String(selectedRule?.set.model_downgrade_threshold ?? 0),
    );
  }, [selectedRuleIndex, selectedRule?.set.model_downgrade_threshold]);

  useEffect(() => {
    setProxySlotInput(selectedRule?.set.proxy_slot == null ? '' : String(selectedRule.set.proxy_slot));
  }, [selectedRuleIndex, selectedRule?.set.proxy_slot]);

  const updateSelectedRule = useCallback((updater: (rule: ImportRule) => void) => {
    setConfig((current) => {
      if (!current.rules[selectedRuleIndex]) return current;
      const next = cloneImportConfig(current);
      updater(next.rules[selectedRuleIndex]!);
      return next;
    });
  }, [selectedRuleIndex]);

  const handleProxyChange = (nextProxyID: number | null, proxy: typeof proxyBinding.proxy) => {
    const nextSlot = proxy?.mode === 'group' ? 'random' : '';
    setProxySlotInput(nextSlot);
    updateSelectedRule((rule) => {
      if (nextProxyID == null) {
        delete rule.set.proxy_id;
        delete rule.set.proxy_slot;
        return;
      }
      rule.set.proxy_id = nextProxyID;
      if (proxy?.mode === 'group') rule.set.proxy_slot = 'random';
      else delete rule.set.proxy_slot;
    });
  };

  const handleProxySlotChange = (value: string) => {
    setProxySlotInput(value);
    const nextBinding = resolveProxyBinding(proxies, proxyID, value);
    if (!nextBinding.valid || !nextBinding.assignment) return;
    updateSelectedRule((rule) => {
      rule.set.proxy_slot = nextBinding.assignment === 'random' ? 'random' : nextBinding.slot;
    });
  };

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

  const appendExample = () => {
    if (loading || (view === 'form' && validationError)) return;
    try {
      // Use the current DSL draft so unsaved edits are preserved in either view.
      const next = view === 'dsl' ? parseImportConfigDSL(dslValue) : cloneImportConfig(config);
      const firstNewIndex = next.rules.length;
      const names = new Set(next.rules.map((rule) => rule.name));
      for (const example of parseImportConfigDSL(ACCOUNT_IMPORT_DSL_EXAMPLE).rules) {
        const baseName = example.name;
        let suffix = 2;
        while (names.has(example.name)) example.name = `${baseName} (${suffix++})`;
        names.add(example.name);
        next.rules.push(example);
      }
      setConfig(next);
      setDSLValue(serializeImportConfigDSL(next));
      setSelectedRuleIndex(firstNewIndex);
      setDSLError('');
      setView('form');
    } catch (error) {
      setDSLError(error instanceof Error ? error.message : String(error));
    }
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
      if (rule.set.priority?.mode === 'sequence') {
        rule.set.priority[key] = value;
      }
    });
  };

  const canSave = !loading && (view === 'dsl' ? dslError === '' : validationError === '');

  return (
    <CommonModal
      className={['ag-account-page-modal', styles.modal].join(' ')}
      description={t('accounts.import_config_description')}
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
      <div className={styles.root}>
        <div className={styles.toolbar}>
          <Tabs
            className={`ag-segmented-tabs ag-segmented-tabs-compact ${styles.viewTabs}`}
            isDisabled={loading}
            selectedKey={view}
            onSelectionChange={(key) => switchView(key as 'form' | 'dsl')}
          >
            <Tabs.List className={styles.viewTabList}>
              <Tabs.Tab className={styles.viewTab} id="form">
                <Tabs.Indicator />
                <span>{t('accounts.import_config_graphical')}</span>
              </Tabs.Tab>
              <Tabs.Tab className={styles.viewTab} id="dsl">
                <Tabs.Separator />
                <Tabs.Indicator />
                <Braces />
                <span>{t('accounts.import_config_advanced')}</span>
              </Tabs.Tab>
            </Tabs.List>
          </Tabs>
          <div className={styles.toolbarActions}>
            <Button className={styles.loadExample} variant="secondary" onPress={appendExample} isDisabled={loading || (view === 'form' && validationError !== '')}>
              {t('accounts.import_config_load_example')}
            </Button>
            <Button
              variant="secondary"
              onPress={() => loadConfig(serializeImportConfigDSL(EMPTY_IMPORT_CONFIG))}
              isDisabled={loading}
            >
              <RotateCcw className="h-4 w-4" />
              {t('accounts.import_config_clear')}
            </Button>
          </div>
        </div>

        {view === 'dsl' ? (
          <div className={styles.dsl}>
            <HeroTextField fullWidth isInvalid={dslError !== ''}>
              <Label>{t('accounts.import_config_dsl')}</Label>
              <TextArea
                className={[styles.dslInput, 'font-mono', 'text-xs', 'leading-5'].join(' ')}
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
          <div className={styles.body}>
            <aside className={styles.sidebar}>
              <div className={styles.sidebarHeader}>
                <span className="text-xs font-medium text-text-secondary">{t('accounts.import_config_rules')}</span>
                <span className="text-[11px] text-text-tertiary">{t('accounts.import_config_first_match')}</span>
              </div>
              <div className={[styles.ruleList, 'ag-simple-multi-select'].join(' ')}>
                {config.rules.map((rule, index) => (
                  <div
                    key={`${rule.name}-${index}`}
                    className={styles.rule}
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
                    <span className={styles.ruleGrip} aria-hidden="true">
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
              <div className={styles.sidebarFooter}>
                <Button className="w-full" variant="secondary" onPress={addRule} isDisabled={loading}>
                  <Plus className="h-4 w-4" />
                  {t('accounts.import_config_add_rule')}
                </Button>
              </div>
            </aside>

            <div className={styles.editor}>
              <div className={styles.editorContent} data-rule-disabled={selectedRule?.enabled === false || undefined}>
              {selectedRule ? (
                <>
                  <div className={styles.ruleHeader}>
                    <NativeCheckbox
                      className={styles.ruleEnabled}
                      isSelected={selectedRule.enabled !== false}
                      onChange={(enabled) => updateSelectedRule((rule) => { rule.enabled = enabled; })}
                    >
                      {t('accounts.import_config_rule_enabled')}
                    </NativeCheckbox>
                    <div className={styles.ruleName}>
                      <HeroTextField fullWidth isDisabled={selectedRule.enabled === false}>
                        <Input
                          aria-label={t('accounts.import_config_rule_name')}
                          disabled={selectedRule.enabled === false}
                          value={selectedRule.name}
                          onChange={(event) => updateSelectedRule((rule) => { rule.name = event.target.value; })}
                        />
                      </HeroTextField>
                    </div>
                    <Button className={styles.ruleAction} variant="secondary" isDisabled={selectedRule.enabled === false} onPress={duplicateRule}>
                      <CopyPlus className="h-4 w-4" />
                      {t('accounts.import_config_duplicate_rule')}
                    </Button>
                    <Button className={styles.ruleAction} variant="secondary" isDisabled={selectedRule.enabled === false} onPress={deleteRule}>
                      <Trash2 className="h-4 w-4 text-danger" />
                      {t('accounts.import_config_delete_rule')}
                    </Button>
                  </div>

                  <fieldset
                    className={styles.ruleFields}
                    disabled={selectedRule.enabled === false}
                    inert={selectedRule.enabled === false}
                    aria-disabled={selectedRule.enabled === false}
                  >
                  <section className={styles.section}>
                    <div className={styles.conditionsHeader}>
                      <div className={styles.conditionsTitle}>
                        <h3 className="text-sm font-semibold text-text">{t('accounts.import_config_conditions')}</h3>
                        <p className="text-xs text-text-tertiary">{t('accounts.import_config_conditions_hint')}</p>
                      </div>
                      <Button
                        className={styles.ruleAction}
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
                        <div key={conditionIndex} className={styles.condition}>
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
                              <ComboBox.Trigger><DropdownIndicator slot="combo-box-trigger-default-icon" /></ComboBox.Trigger>
                            </ComboBox.InputGroup>
                            <ComboBox.Popover className={styles.fieldPopover}>
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
                            <Trash2 className="h-4 w-4 text-danger" />
                          </Button>
                        </div>
                      );
                    })}
                  </section>

                  <section className={styles.section}>
                    <h3 className="text-sm font-semibold text-text">{t('accounts.import_config_assignments')}</h3>

                    <div className={styles.assignments}>
                      <div className={styles.priority}>
                        <div className={styles.labeledControl}>
                          <Label>{t('accounts.import_config_priority')}</Label>
                          <SimpleSelect
                            ariaLabel={t('accounts.import_config_priority')}
                            fullWidth
                            items={[
                              { key: 'fixed', label: t('accounts.import_config_priority_fixed') },
                              { key: 'sequence', label: t('accounts.priority_sequence') },
                            ]}
                            selectedKey={displayedPriority.mode}
                            onSelectionChange={(key) => {
                              setPriorityMode(key as 'fixed' | 'sequence');
                            }}
                          />
                        </div>
                        <HeroTextField fullWidth>
                          <Label>{t('accounts.priority')}</Label>
                          <Input
                            aria-label={t('accounts.priority')}
                            className="w-full"
                            type="number"
                            min={IMPORT_PRIORITY_MIN}
                            max={IMPORT_PRIORITY_MAX}
                            disabled={displayedPriority.mode !== 'fixed'}
                            placeholder={t('accounts.import_config_priority_placeholder')}
                            value={selectedRule.set.priority?.mode === 'fixed'
                              ? String(selectedRule.set.priority.value)
                              : ''}
                            onChange={(event) => {
                              const raw = event.target.value.trim();
                              updateSelectedRule((rule) => {
                                if (rule.set.priority?.mode !== 'fixed') {
                                  rule.set.priority = { mode: 'fixed', value: 50 };
                                }
                                if (raw === '') {
                                  delete rule.set.priority;
                                  return;
                                }
                                rule.set.priority.value = parseNumber(raw, 0);
                              });
                            }}
                          />
                        </HeroTextField>
                      </div>
                      <HeroTextField fullWidth isInvalid={!schedulingWeightValid}>
                        <Label>{t('accounts.scheduling_weight')}</Label>
                        <Input aria-label={t('accounts.scheduling_weight')} type="number" min={0} max={MAX_SCHEDULING_WEIGHT} step={1}
                          value={schedulingWeightInput}
                          onChange={(event) => {
                            const raw = event.target.value;
                            setSchedulingWeightInput(raw);
                            const parsed = parseSchedulingWeightInput(raw);
                            if (raw.trim() === '' || parsed != null) updateSelectedRule((rule) => {
                              if (parsed == null) delete rule.set.scheduling_weight;
                              else rule.set.scheduling_weight = parsed;
                            });
                          }} />
                        {!schedulingWeightValid && <p className="mt-1 text-[11px] leading-4 text-danger">{t('accounts.scheduling_weight_invalid')}</p>}
                      </HeroTextField>
                      <HeroTextField fullWidth>
                        <Label>{t('accounts.concurrency')}</Label>
                        <Input
                          aria-label={t('accounts.import_config_capacity')}
                          className="w-full"
                          type="number"
                          min={0}
                          placeholder={t('accounts.import_config_capacity_placeholder')}
                          value={selectedRule.set.max_concurrency != null
                            ? String(selectedRule.set.max_concurrency)
                            : ''}
                          onChange={(event) => {
                            const raw = event.target.value.trim();
                            updateSelectedRule((rule) => {
                              if (raw === '') {
                                delete rule.set.max_concurrency;
                                return;
                              }
                              rule.set.max_concurrency = Math.max(0, parseNumber(raw, 0));
                            });
                          }}
                        />
                      </HeroTextField>
                      <HeroTextField fullWidth isInvalid={!modelDowngradeThresholdValid}>
                        <Label>{t('accounts.model_downgrade_threshold')}</Label>
                          <Input
                            aria-label={t('accounts.model_downgrade_threshold')}
                            className="w-full"
                            type="text"
                            inputMode="decimal"
                            placeholder={t('accounts.import_config_threshold_placeholder')}
                            value={modelDowngradeThresholdInput}
                            onChange={(event) => {
                              const raw = event.target.value;
                              setModelDowngradeThresholdInput(raw);
                              const parsed = parseModelDowngradeThresholdInput(raw);
                              if (parsed == null) return;
                              updateSelectedRule((rule) => {
                                rule.set.model_downgrade_threshold = parsed;
                              });
                            }}
                          />
                        {!modelDowngradeThresholdValid ? (
                          <p className="mt-1 text-[11px] leading-4 text-danger">
                            {t('accounts.model_downgrade_threshold_invalid')}
                          </p>
                        ) : null}
                      </HeroTextField>
                      {displayedPriority.mode === 'sequence' ? (
                        <div className={styles.sequenceFields}>
                          <HeroTextField fullWidth>
                            <Label>{t('accounts.priority_sequence_initial')}</Label>
                            <Input type="number" value={String(displayedPriority.initial)} onChange={(event) => setSequenceValue('initial', parseNumber(event.target.value, 0))} />
                          </HeroTextField>
                          <HeroTextField fullWidth>
                            <Label>{t('accounts.priority_sequence_step')}</Label>
                            <Input type="number" value={String(displayedPriority.step)} onChange={(event) => setSequenceValue('step', parseNumber(event.target.value, 0))} />
                          </HeroTextField>
                          <HeroTextField fullWidth>
                            <Label>{t('accounts.priority_sequence_group_size')}</Label>
                            <Input type="number" min={1} value={String(displayedPriority.group_size)} onChange={(event) => setSequenceValue('group_size', Math.max(1, parseNumber(event.target.value, 1)))} />
                          </HeroTextField>
                          <HeroTextField fullWidth>
                            <Label>{t('accounts.import_config_priority_min')}</Label>
                            <Input type="number" min={IMPORT_PRIORITY_MIN} max={IMPORT_PRIORITY_MAX} value={String(displayedPriority.min ?? IMPORT_PRIORITY_MIN)} onChange={(event) => setSequenceValue('min', parseNumber(event.target.value, IMPORT_PRIORITY_MIN))} />
                          </HeroTextField>
                          <HeroTextField fullWidth>
                            <Label>{t('accounts.import_config_priority_max')}</Label>
                            <Input type="number" min={IMPORT_PRIORITY_MIN} max={IMPORT_PRIORITY_MAX} value={String(displayedPriority.max ?? IMPORT_PRIORITY_MAX)} onChange={(event) => setSequenceValue('max', parseNumber(event.target.value, IMPORT_PRIORITY_MAX))} />
                          </HeroTextField>
                        </div>
                      ) : null}
                    </div>

                    <div className={styles.fieldGroup}>
                      <ProxyBindingFields
                        emptyLabel={t('accounts.no_proxy')}
                        onProxyChange={handleProxyChange}
                        onSlotChange={handleProxySlotChange}
                        proxies={proxies}
                        proxyId={proxyID}
                        slotInput={proxySlotInput}
                      />
                    </div>

                    <div className={styles.fieldGroup}>
                      <p className="text-sm font-semibold text-text">
                        {t('accounts.import_config_groups_assignment')}
                      </p>
                      <div className={styles.groups}>
                        {visibleGroups.map((group) => (
                          <NativeCheckbox
                            key={group.id}
                            isSelected={selectedRule.set.group_ids?.includes(group.id) ?? false}
                            onChange={(selected) => updateSelectedRule((rule) => {
                              const ids = rule.set.group_ids ?? [];
                              const next = selected
                                ? Array.from(new Set([...ids, group.id]))
                                : ids.filter((id) => id !== group.id);
                              rule.set.group_ids = next;
                            })}
                          >
                            <span className="text-sm text-text">
                              {group.name}
                              <span className="ml-1 text-xs text-text-tertiary">#{group.id} › {group.platform}</span>
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
                  </fieldset>
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
