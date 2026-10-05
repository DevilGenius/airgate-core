import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ImportConfigModal } from './ImportConfigModal';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock('../../../shared/components/CommonModal', () => ({
  CommonModal: ({ children, footer }: { children: ReactNode; footer: ReactNode }) => (
    <div role="dialog">{children}{footer}</div>
  ),
}));

function renderRule(enabled: boolean) {
  const onSubmit = vi.fn();
  render(<ImportConfigModal open loading={false} groups={[]} proxies={[]} onClose={() => {}} onSubmit={onSubmit}
    dsl={JSON.stringify({ version: 1, rules: [{ name: 'Example', enabled, when: [], set: { model_downgrade_threshold: 0 } }] })} />);
  return onSubmit;
}

describe('disabled import rules', () => {
  it('can duplicate and delete a disabled rule without enabling it', () => {
    const onSubmit = renderRule(false);
    const duplicate = screen.getByRole('button', { name: 'accounts.import_config_duplicate_rule' });
    expect(duplicate).toBeEnabled();
    fireEvent.click(duplicate);
    expect(screen.getByRole('checkbox', { name: 'accounts.import_config_rule_enabled' })).not.toBeChecked();
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    const copied = JSON.parse(onSubmit.mock.calls[0]?.[0] as string);
    expect(copied.rules).toHaveLength(2);
    expect(copied.rules.every((rule: { enabled: boolean }) => rule.enabled === false)).toBe(true);
    const remove = screen.getByRole('button', { name: 'accounts.import_config_delete_rule' });
    expect(remove).toBeEnabled();
    fireEvent.click(remove);
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(JSON.parse(onSubmit.mock.calls[1]?.[0] as string).rules).toEqual([copied.rules[0]]);
  });

  it('keeps validation feedback outside the disabled and inert fields', () => {
    renderRule(true);
    fireEvent.change(screen.getByRole('textbox', { name: 'accounts.import_config_rule_name' }), { target: { value: '' } });
    fireEvent.click(screen.getByRole('checkbox', { name: 'accounts.import_config_rule_enabled' }));
    const alert = screen.getByRole('alert');
    expect(alert).toBeVisible();
    expect(alert.closest('fieldset, [inert], [aria-disabled="true"]')).toBeNull();
    expect(screen.getByRole('textbox', { name: 'accounts.import_config_rule_name' })).toBeDisabled();
  });
});
