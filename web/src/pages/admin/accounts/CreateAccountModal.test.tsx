import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CreateAccountModal } from './CreateAccountModal';
import type { CredentialSchemaResp, GroupResp, ProxyResp } from '../../../shared/types';

const mocks = vi.hoisted(() => ({
  pluginForm: null as React.ComponentType<any> | null,
}));

vi.mock('@heroui/react', async () => import('../../../test/herouiMock'));

vi.mock('react-i18next', () => ({
  initReactI18next: {
    init: () => {},
    type: '3rdParty',
  },
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
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
    isSelected,
    label,
    onChange,
  }: {
    isSelected: boolean;
    label?: React.ReactNode;
    onChange: (checked: boolean) => void;
  }) => (
    <label>
      {label}
      <input
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
        <option key={item.key} value={item.key}>{item.key || 'none'}</option>
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
    usePluginAccountForm: vi.fn(() => ({ Form: mocks.pluginForm, pluginId: mocks.pluginForm ? 'plugin-id' : '' })),
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
  ],
  fields: [],
};

const groups: GroupResp[] = [
  {
    account_active: 0,
    account_disabled: 0,
    account_error: 0,
    account_total: 0,
    capacity_total: 0,
    capacity_used: 0,
    created_at: '2026-06-20T00:00:00Z',
    id: 3,
    is_exclusive: false,
    name: 'Default',
    platform: 'openai',
    rate_multiplier: 1,
    sort_weight: 0,
    status_visible: true,
    subscription_type: 'standard',
    today_cost: 0,
    total_cost: 0,
    updated_at: '2026-06-20T00:00:00Z',
  },
];

const proxies = [
  {
    address: '127.0.0.1',
    created_at: '2026-06-20T00:00:00Z',
    id: 9,
    name: 'Local',
    port: 8080,
    protocol: 'http',
    status: 'active',
    updated_at: '2026-06-20T00:00:00Z',
    username: '',
  } as ProxyResp,
];

vi.mock('@tanstack/react-query', () => ({
  useQuery: vi.fn(({ enabled, queryKey }: { enabled?: boolean; queryKey: readonly unknown[] }) => {
    if (queryKey[0] === 'credentials-schema') {
      return enabled === false ? { data: undefined } : { data: credentialSchema };
    }
    if (queryKey[0] === 'groups-all') {
      return { data: { list: groups, total: groups.length } };
    }
    if (queryKey[0] === 'proxies-all') {
      return { data: { list: proxies, total: proxies.length } };
    }
    return { data: undefined };
  }),
}));

function formElement(container: HTMLElement) {
  const form = container.querySelector('form');
  if (!form) throw new Error('form not found');
  return form;
}

describe('CreateAccountModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.pluginForm = null;
  });

  it('submits schema-based account credentials and advanced options', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    const { container } = render(
      <CreateAccountModal
        loading={false}
        open
        platforms={['openai', 'claude']}
        onClose={() => {}}
        onSubmit={onSubmit}
      />,
    );

    await user.selectOptions(screen.getByLabelText('accounts.platform'), 'openai');
    await waitFor(() => expect(screen.getByPlaceholderText('sk-...')).toBeTruthy());

    fireEvent.change(container.querySelector<HTMLInputElement>('input[name="name"]')!, {
      target: { value: 'Primary Account' },
    });
    fireEvent.change(screen.getByPlaceholderText('sk-...'), {
      target: { value: 'sk-account' },
    });
    await user.selectOptions(screen.getByLabelText('accounts.proxy'), '9');
    const checkboxInputs = container.querySelectorAll<HTMLInputElement>('input[type="checkbox"]');
    await user.click(checkboxInputs[0]!);
    await user.click(checkboxInputs[1]!);

    const numberInputs = container.querySelectorAll<HTMLInputElement>('input[type="number"]');
    fireEvent.change(numberInputs[0]!, { target: { value: '6' } });
    fireEvent.change(numberInputs[1]!, { target: { value: '1.25' } });
    await user.click(screen.getByText('Default'));

    fireEvent.submit(formElement(container));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      credentials: { api_key: 'sk-account' },
      email: null,
      extra: { msg_lock_enabled: true },
      group_ids: [3],
      max_concurrency: 6,
      name: 'Primary Account',
      platform: 'openai',
      proxy_id: 9,
      rate_multiplier: 1.25,
      type: 'apikey',
      upstream_is_pool: true,
    }));
  });

  it('does not submit when platform, name or rate multiplier are invalid', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    const { container } = render(
      <CreateAccountModal
        loading={false}
        open
        platforms={['openai']}
        onClose={() => {}}
        onSubmit={onSubmit}
      />,
    );

    fireEvent.submit(formElement(container));
    expect(onSubmit).not.toHaveBeenCalled();

    await user.selectOptions(screen.getByLabelText('accounts.platform'), 'openai');
    fireEvent.change(container.querySelector<HTMLInputElement>('input[name="name"]')!, {
      target: { value: 'Invalid Rate Account' },
    });
    const numberInputs = container.querySelectorAll<HTMLInputElement>('input[type="number"]');
    fireEvent.change(numberInputs[1]!, { target: { value: '0' } });

    fireEvent.submit(formElement(container));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('supports plugin custom forms and suggested names', async () => {
    const onSubmit = vi.fn();
    mocks.pluginForm = ({ onChange, onSuggestedName }: any) => (
      <button
        type="button"
        onClick={() => {
          onSuggestedName('Suggested Account');
          onChange({ plugin_token: 'plugin-secret', email: ' Plugin@Example.COM ' });
        }}
      >
        Fill plugin form
      </button>
    );

    const user = userEvent.setup();
    const { container } = render(
      <CreateAccountModal
        loading={false}
        open
        platforms={['openai']}
        onClose={() => {}}
        onSubmit={onSubmit}
      />,
    );

    await user.selectOptions(screen.getByLabelText('accounts.platform'), 'openai');
    await user.click(screen.getByRole('button', { name: 'Fill plugin form' }));
    fireEvent.submit(formElement(container));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      credentials: { email: 'plugin@example.com', plugin_token: 'plugin-secret' },
      email: 'plugin@example.com',
      name: 'Suggested Account',
      platform: 'openai',
    }));
  });
});
