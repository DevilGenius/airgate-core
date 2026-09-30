import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { EditAccountModal } from './EditAccountModal';
import { BulkEditAccountModal } from './BulkEditAccountModal';
import type { AccountResp, CredentialSchemaResp } from '../../../shared/types';

vi.mock('@heroui/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@heroui/react')>();
  return {
    ...await import('../../../test/herouiMock'),
    ComboBox: actual.ComboBox,
    ListBox: actual.ListBox,
    Input: actual.Input,
  };
});

vi.mock('react-i18next', () => ({
  initReactI18next: {
    init: () => {},
    type: '3rdParty',
  },
  useTranslation: () => ({
    t: (key: string, fallback?: unknown) => typeof fallback === 'string' ? fallback : key,
  }),
}));

vi.mock('../../../shared/components/CommonModal', () => ({
  CommonModal: ({
    children,
    footer,
    state,
    title,
  }: {
    children: React.ReactNode;
    footer: React.ReactNode;
    state: { isOpen?: boolean };
    title: string;
  }) => (state.isOpen === false ? null : (
    <div aria-label={title} role="dialog">
      <h2>{title}</h2>
      {children}
      <footer>{footer}</footer>
    </div>
  )),
}));

vi.mock('../../../shared/components/NativeSwitch', () => ({
  NativeSwitch: ({
    isDisabled,
    isSelected,
    label,
    onChange,
  }: {
    isDisabled?: boolean;
    isSelected: boolean;
    label?: React.ReactNode;
    onChange: (checked: boolean) => void;
  }) => (
    <label>
      {label}
      <input
        role="switch"
        disabled={isDisabled}
        checked={isSelected}
        type="checkbox"
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
    </label>
  ),
}));

vi.mock('../../../shared/components/SimpleSelect', () => ({
  SimpleSelect: ({
    ariaLabel,
    items,
    onSelectionChange,
    selectedKey,
  }: {
    ariaLabel: string;
    items: Array<{ key: string; label: React.ReactNode }>;
    onSelectionChange: (key: string) => void;
    selectedKey?: string | number | null;
  }) => (
    <select
      aria-label={ariaLabel}
      value={selectedKey == null ? '' : String(selectedKey)}
      onChange={(event) => onSelectionChange(event.currentTarget.value)}
    >
      {items.map((item) => (
        <option key={item.key} value={item.key}>{item.label}</option>
      ))}
    </select>
  ),
}));

vi.mock('../../../shared/hooks/usePlatforms', () => ({
  usePlatforms: () => ({
    platformName: (platform: string) => `Platform ${platform}`,
  }),
}));

vi.mock('./accountUtils', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./accountUtils')>();
  return {
    ...actual,
    createPluginOAuthBridge: vi.fn(() => undefined),
    usePluginAccountForm: vi.fn(() => ({
      Form: null,
      loaded: true,
      pluginId: '',
    })),
  };
});

const credentialSchema: CredentialSchemaResp = {
  account_types: [
    {
      description: 'API key account',
      fields: [
        { key: 'api_key', label: 'API Key', placeholder: 'sk-...', required: true, type: 'password' },
      ],
      key: 'apikey',
      label: 'API Key',
    },
    {
      description: 'OAuth account',
      fields: [
        { key: 'access_token', label: 'Access Token', placeholder: 'token', required: true, type: 'password' },
      ],
      key: 'oauth',
      label: 'OAuth',
    },
  ],
  fields: [],
};

vi.mock('@tanstack/react-query', () => ({
  useQuery: vi.fn(({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === 'credentials-schema') return { data: credentialSchema };
    if (queryKey[0] === 'groups-all') return { data: { list: [], total: 0 } };
    if (queryKey[0] === 'proxies-all') return { data: { list: [], total: 0 } };
    return { data: undefined };
  }),
}));

function account(overrides: Partial<AccountResp> = {}): AccountResp {
  return {
    created_at: '2026-08-04T00:00:00Z',
    credentials: { api_key: 'sk-existing' },
    current_concurrency: 0,
    email: null,
    extra: {},
    group_ids: [],
    id: 1,
    max_concurrency: 5,
    model_policy: {},
    name: 'Primary Account',
    platform: 'openai',
    priority: 0,
    rate_multiplier: 1,
    state: 'active',
    type: 'apikey',
    updated_at: '2026-08-04T00:00:00Z',
    upstream_is_pool: false,
    ...overrides,
  };
}

function renderModal(item: AccountResp, onSubmit = vi.fn()) {
  render(
    <EditAccountModal
      account={item}
      loading={false}
      open
      onClose={() => {}}
      onSubmit={onSubmit}
    />,
  );
  return onSubmit;
}

describe('EditAccountModal model policy', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('edits a locked plan manually without a dispatch control or state patch', () => {
    const onSubmit = renderModal(account({
      credentials: { api_key: 'secret', plan_type: 'plus' },
      extra: { plan_type_locked: true, keep: 'value' },
      state: 'disabled',
    }));
    expect(screen.queryByText('accounts.enable_dispatch')).not.toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'accounts.plan_type_locked' })).toBeChecked();
    fireEvent.change(screen.getByRole('combobox', { name: 'accounts.plan_type' }), { target: { value: ' team ' } });
    fireEvent.click(screen.getByRole('checkbox', { name: '池模式' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      plan_type: 'team', upstream_is_pool: true, extra: { plan_type_locked: true, keep: 'value' },
    }));
    expect(onSubmit.mock.calls[0]?.[0]).not.toHaveProperty('state');
  });

  it('can clear plan_type explicitly', () => {
    const onSubmit = renderModal(account({ credentials: { api_key: 'secret', plan_type: 'plus' } }));
    fireEvent.change(screen.getByRole('combobox', { name: 'accounts.plan_type' }), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ plan_type: '' }));
  });

  it('shows the existing account model policy', () => {
    renderModal(account({
      model_policy: {
        allow: ['gpt-5.4', 'gpt-5.4-*'],
        deny: ['gpt-5.4-nano'],
      },
    }));

    expect(screen.getByLabelText('accounts.model_allowlist')).toHaveValue('gpt-5.4\ngpt-5.4-*');
    expect(screen.getByLabelText('accounts.model_denylist')).toHaveValue('gpt-5.4-nano');
  });

  it('offers upstream plan presets and allows editing a selected preset', async () => {
    const user = userEvent.setup();
    const onSubmit = renderModal(account());
    const input = screen.getByRole('combobox', { name: 'accounts.plan_type' });
    await user.click(screen.getByRole('button', { name: 'accounts.plan_type' }));
    expect((await screen.findAllByRole('option')).map((option) => option.textContent))
      .toEqual(['free', 'plus', 'pro', 'team', 'self_serve_business_prolite', 'prolite', 'k12', 'enterprise']);
    await user.click(screen.getByRole('option', { name: 'self_serve_business_prolite' }));
    expect(input).toHaveValue('self_serve_business_prolite');
    fireEvent.change(input, { target: { value: 'custom-team-plan' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ plan_type: 'custom-team-plan' }));
  });

  it('submits an OAuth account model policy', () => {
    const onSubmit = renderModal(account({
      credentials: { access_token: 'oauth-token' },
      type: 'oauth',
    }));

    fireEvent.change(screen.getByLabelText('accounts.model_allowlist'), {
      target: { value: 'gpt-5.4\n gpt-5.6 ' },
    });
    fireEvent.change(screen.getByLabelText('accounts.model_denylist'), {
      target: { value: 'gpt-5.4-nano' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      model_policy: {
        allow: ['gpt-5.4', 'gpt-5.6'],
        deny: ['gpt-5.4-nano'],
      },
      type: 'oauth',
    }));
  });

  it('submits an API Key account model policy', () => {
    const onSubmit = renderModal(account());

    fireEvent.change(screen.getByLabelText('accounts.model_allowlist'), {
      target: { value: 'gpt-5.4, gpt-5.4-mini' },
    });
    fireEvent.change(screen.getByLabelText('accounts.model_denylist'), {
      target: { value: 'gpt-5.4-nano\n' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      model_policy: {
        allow: ['gpt-5.4', 'gpt-5.4-mini'],
        deny: ['gpt-5.4-nano'],
      },
      type: 'apikey',
    }));
  });

  it('submits an empty policy to clear the current model restrictions', () => {
    const onSubmit = renderModal(account({
      model_policy: {
        allow: ['gpt-5.4*'],
        deny: ['gpt-5.4-nano'],
      },
    }));

    fireEvent.change(screen.getByLabelText('accounts.model_allowlist'), {
      target: { value: '' },
    });
    fireEvent.change(screen.getByLabelText('accounts.model_denylist'), {
      target: { value: '' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      model_policy: {},
    }));
  });
});

describe('BulkEditAccountModal plan controls', () => {
  it('uses four switches and patches only explicitly edited fields', () => {
    const onSubmit = vi.fn();
    render(<BulkEditAccountModal open count={2} loading={false} initialPlanType="plus" initialPlanTypeLocked initialPoolMode onClose={() => {}} onSubmit={onSubmit} />);
    expect(screen.getAllByRole('switch')).toHaveLength(4);
    expect(screen.queryByText('accounts.rate_multiplier')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'common.save' })).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: 'accounts.plan_type_label' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'accounts.plan_type_label' }), { target: { value: ' pro ' } });
    fireEvent.click(screen.getByRole('switch', { name: 'accounts.plan_type_locked' }));
    fireEvent.click(screen.getByRole('switch', { name: 'accounts.upstream_is_pool' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith({ plan_type: 'pro', upstream_is_pool: false, extra: { plan_type_locked: false } });
  });

  it('keeps dispatch opt-in separate from the other switches', () => {
    const onSubmit = vi.fn();
    render(<BulkEditAccountModal open count={2} loading={false} onClose={() => {}} onSubmit={onSubmit} />);
    expect(screen.getByRole('switch', { name: 'accounts.dispatch_enabled' })).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox', { name: 'accounts.dispatch_toggle' }));
    fireEvent.click(screen.getByRole('switch', { name: 'accounts.dispatch_enabled' }));
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
    expect(onSubmit).toHaveBeenCalledWith({ state: 'disabled' });
  });
});
