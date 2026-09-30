import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { GroupFormModal } from './EditGroupModal';
import type { GroupResp } from '../../../shared/types';

vi.mock('@heroui/react', async () => import('../../../test/herouiMock'));
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: undefined, isLoading: false }) }));
vi.mock('../../../shared/components/DialogTriggerShim', () => ({ DialogTriggerShim: () => null }));
vi.mock('../../../shared/hooks/usePlatforms', () => ({ usePlatforms: () => ({ oauthPlanFilters: [
  { id: 'oauth_plan:openai:plus', platform: 'openai', planLabel: 'Plus' },
  { id: 'oauth_plan:kiro:power', platform: 'kiro', planLabel: 'Power' },
  { id: 'oauth_plan:claude:max', platform: 'claude', planLabel: 'Max' },
] }) }));
vi.mock('react-i18next', () => ({
  initReactI18next: { init: () => {}, type: '3rdParty' },
  useTranslation: () => ({ t: (key: string) => key }),
}));

function mount(settings: Record<string, string> = {}, policies: GroupResp['account_type_model_policies'] = {}, platform = 'openai') {
  const onSubmit = vi.fn();
  const group: GroupResp = {
    id: 18, name: 'Group', platform, rate_multiplier: 1,
    is_exclusive: false,
    status_visible: true,
    subscription_type: 'standard',
    sort_weight: 0,
    created_at: '2026-09-28T00:00:00Z',
    updated_at: '2026-09-28T00:00:00Z',
    plugin_settings: { openai: { existing_setting: 'keep', ...settings } },
    account_type_model_policies: policies,
  };
  render(<GroupFormModal open title="Edit group" group={group} onClose={vi.fn()} onSubmit={onSubmit} loading={false} platforms={['openai']} />);
  return onSubmit;
}

describe('Basispoints API key standard billing option', () => {
  it.each([['kiro', 'Power', 'power'], ['claude', 'Max', 'max']])('edits platform plans and Unknown for %s', (platform, label, key) => {
    const onSubmit = mount({}, {}, platform);
    expect(screen.queryByRole('textbox', { name: 'groups.model_allowlist(Plus)' })).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole('textbox', { name: 'groups.model_allowlist(' + label + ')' }), { target: { value: 'supported-*' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'groups.model_denylist(OAuth Unknown)' }), { target: { value: 'restricted-*' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ account_type_model_policies: {
      [key]: { allow: ['supported-*'] }, oauth: { deny: ['restricted-*'] },
    } }));
  });
  it('labels the existing oauth policy as OAuth Unknown and preserves its key on save', () => {
    const onSubmit = mount({}, { oauth: { deny: ['blocked-*'] }, plus: { allow: ['plus-*'] } });
    const deny = screen.getByRole('textbox', { name: 'groups.model_denylist(OAuth Unknown)' });
    expect(deny).toHaveValue('blocked-*');
    expect(screen.queryByText(/OAuth 缺省/)).not.toBeInTheDocument();
    fireEvent.change(deny, { target: { value: 'unknown-blocked-*' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ account_type_model_policies: {
      oauth: { deny: ['unknown-blocked-*'] }, plus: { allow: ['plus-*'] },
    } }));
  });
  it('defaults off, follows the BAS toggle and persists without losing other settings', () => {
    const onSubmit = mount();
    const bas = screen.getByRole('checkbox', { name: 'groups.openai_basispoints' });
    const policy = screen.getByRole('checkbox', { name: 'groups.openai_basispoints_standard_key_billing' });
    expect(policy).not.toBeChecked();
    expect(policy).toBeDisabled();
    fireEvent.click(bas);
    expect(policy).toBeEnabled();
    fireEvent.click(policy);
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      plugin_settings: { openai: expect.objectContaining({ basispoints: 'true', basispoints_standard_key_billing: 'true', existing_setting: 'keep' }) },
    }));
  });

  it('loads saved values and disables the dependent option when BAS is off', () => {
    const onSubmit = mount({ basispoints: 'true', basispoints_standard_key_billing: 'true' });
    const policy = screen.getByRole('checkbox', { name: 'groups.openai_basispoints_standard_key_billing' });
    expect(policy).toBeChecked();
    fireEvent.click(screen.getByRole('checkbox', { name: 'groups.openai_basispoints' }));
    expect(policy).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      plugin_settings: { openai: expect.objectContaining({ basispoints: 'false', basispoints_standard_key_billing: 'true' }) },
    }));
  });
});
