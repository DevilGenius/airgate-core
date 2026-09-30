import { ComboBox, Input, Label, ListBox } from '@heroui/react';
import styles from './AccountPlanTypeInput.module.css';
import { DropdownIndicator } from '../../../shared/components/DropdownIndicator';

// Editable suggestions, not declarations of a platform's actual subscription.
// Include both the upstream ProLite value and its normalized shorthand.
export const ACCOUNT_PLAN_PRESETS = ['free', 'plus', 'pro', 'team', 'self_serve_business_prolite', 'prolite', 'k12', 'enterprise'] as const;
const PLAN_OPTIONS = ACCOUNT_PLAN_PRESETS.map((value) => ({ id: value, label: value }));

export function AccountPlanTypeInput({ value, onChange, label, disabled = false, showLabel = false }: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  disabled?: boolean;
  showLabel?: boolean;
}) {
  return (
    <ComboBox
      fullWidth
      allowsCustomValue
      className={styles.root}
      aria-label={label}
      name="plan_type"
      menuTrigger="manual"
      isDisabled={disabled}
      inputValue={value}
      items={PLAN_OPTIONS}
      selectedKey={PLAN_OPTIONS.find((option) => option.id === value)?.id ?? null}
      onInputChange={onChange}
      onSelectionChange={(key) => { if (key != null) onChange(String(key)); }}
    >
      {showLabel ? <Label>{label}</Label> : null}
      <ComboBox.InputGroup>
        <Input autoComplete="off" />
        <ComboBox.Trigger aria-label={label}>
          <DropdownIndicator slot="combo-box-trigger-default-icon" />
        </ComboBox.Trigger>
      </ComboBox.InputGroup>
      <ComboBox.Popover className={styles.popover} placement="bottom start" offset={6}>
        <ListBox className={styles.list}>
          {(option: typeof PLAN_OPTIONS[number]) => (
            <ListBox.Item className={styles.option} id={option.id} textValue={option.label}>
              <span className={styles.value}>{option.label}</span>
              <ListBox.ItemIndicator className={styles.indicator} />
            </ListBox.Item>
          )}
        </ListBox>
      </ComboBox.Popover>
    </ComboBox>
  );
}
