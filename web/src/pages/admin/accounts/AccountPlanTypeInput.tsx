import { useId } from 'react';
import { Input } from '@heroui/react';

// Keep the canonical names aligned with backend/internal/plantype/plan_type.go.
export const ACCOUNT_PLAN_PRESETS = ['free', 'plus', 'team', 'prolite', 'pro', 'k12', 'enterprise'] as const;

export function AccountPlanTypeInput({ value, onChange, label, disabled = false }: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  disabled?: boolean;
}) {
  const listId = useId();
  return (
    <>
      <Input
        aria-label={label}
        autoComplete="off"
        name="plan_type"
        list={listId}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
      <datalist id={listId}>
        {ACCOUNT_PLAN_PRESETS.map((plan) => <option key={plan} value={plan} />)}
      </datalist>
    </>
  );
}
