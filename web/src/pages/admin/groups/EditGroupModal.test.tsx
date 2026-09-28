import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { GroupFormModal } from './EditGroupModal';
import type { GroupResp } from '../../../shared/types';

vi.mock('@heroui/react', async () => import('../../../test/herouiMock'));
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: undefined, isLoading: false }) }));
vi.mock('../../../shared/components/DialogTriggerShim', () => ({ DialogTriggerShim: () => null }));
vi.mock('react-i18next', () => ({
  initReactI18next: { init: () => {}, type: '3rdParty' },
  useTranslation: () => ({ t: (key: string) => key }),
}));

function mount(settings: Record<string, string> = {}) {
  const onSubmit = vi.fn();
  const group: GroupResp = {
    id: 18, name: 'OpenAI', platform: 'openai', rate_multiplier: 1,
    is_exclusive: false,
    status_visible: true,
    subscription_type: 'standard',
    sort_weight: 0,
    created_at: '2026-09-28T00:00:00Z',
    updated_at: '2026-09-28T00:00:00Z',
    plugin_settings: { openai: { existing_setting: 'keep', ...settings } },
  };
  render(<GroupFormModal open title="Edit group" group={group} onClose={vi.fn()} onSubmit={onSubmit} loading={false} platforms={['openai']} />);
  return onSubmit;
}

describe('Basispoints API key standard billing option', () => {
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
