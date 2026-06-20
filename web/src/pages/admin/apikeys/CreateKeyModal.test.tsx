import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CreateKeyModal } from './CreateKeyModal';
import type { GroupResp } from '../../../shared/types';

const mocks = vi.hoisted(() => ({
  toast: vi.fn(),
  user: {
    group_rates: {
      2: 0.8,
    },
  },
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

vi.mock('../../../app/providers/AuthProvider', () => ({
  useAuth: () => ({ user: mocks.user }),
}));

vi.mock('../../../shared/ui', () => ({
  useToast: () => ({ toast: mocks.toast }),
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

vi.mock('../../../shared/components/CommonDatePicker', () => ({
  CommonDatePicker: ({
    label,
    onChange,
    value,
  }: {
    label: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <input
      aria-label={label}
      type="date"
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
    />
  ),
}));

vi.mock('../../../shared/components/NativeSwitch', () => ({
  NativeSwitch: ({
    ariaLabel,
    isSelected,
    onChange,
  }: {
    ariaLabel: string;
    isSelected: boolean;
    onChange: (value: boolean) => void;
  }) => (
    <label>
      {ariaLabel}
      <input
        aria-label={ariaLabel}
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
      <option value="" />
      {items.map((item) => (
        <option key={item.key} value={item.key}>{item.key}</option>
      ))}
    </select>
  ),
}));

function group(overrides: Partial<GroupResp>): GroupResp {
  return {
    account_active: 0,
    account_disabled: 0,
    account_error: 0,
    account_total: 0,
    capacity_total: 0,
    capacity_used: 0,
    created_at: '2026-06-20T00:00:00Z',
    id: 0,
    is_exclusive: false,
    name: '',
    platform: '',
    rate_multiplier: 1,
    sort_weight: 0,
    status_visible: true,
    subscription_type: 'standard',
    today_cost: 0,
    total_cost: 0,
    updated_at: '2026-06-20T00:00:00Z',
    ...overrides,
  };
}

const groups: GroupResp[] = [
  group({ id: 1, name: 'Default', platform: 'openai', rate_multiplier: 1 }),
  group({ id: 2, name: 'Discounted', platform: 'claude', rate_multiplier: 1.2 }),
];

describe('admin CreateKeyModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('submits parsed API key form data', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();

    const { container } = render(
      <CreateKeyModal
        groups={groups}
        loading={false}
        open
        onClose={() => {}}
        onSubmit={onSubmit}
      />,
    );

    await user.type(screen.getByPlaceholderText('api_keys.name_placeholder'), 'Production');
    await user.selectOptions(screen.getByLabelText('api_keys.group'), '2');

    const numberInputs = container.querySelectorAll<HTMLInputElement>('input[type="number"]');
    fireEvent.change(numberInputs[0]!, { target: { value: '25.5' } });
    fireEvent.change(numberInputs[1]!, { target: { value: '1.5' } });
    fireEvent.change(numberInputs[2]!, { target: { value: '4' } });

    await user.click(screen.getByLabelText('api_keys.balance_alert_enabled'));
    await user.type(screen.getByPlaceholderText('name@example.com'), 'ops@example.com');
    const updatedNumberInputs = container.querySelectorAll<HTMLInputElement>('input[type="number"]');
    fireEvent.change(updatedNumberInputs[3]!, { target: { value: '3.5' } });

    const ipInputs = screen.getAllByPlaceholderText('api_keys.ip_placeholder');
    fireEvent.change(ipInputs[0]!, { target: { value: '10.0.0.1\n\n10.0.0.2' } });
    fireEvent.change(ipInputs[1]!, { target: { value: '192.168.1.1' } });
    fireEvent.change(screen.getByLabelText('api_keys.expire_time'), { target: { value: '2026-07-01' } });

    await user.click(screen.getByRole('button', { name: /common\.create/ }));

    expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      balance_alert_email: 'ops@example.com',
      balance_alert_enabled: true,
      balance_alert_threshold: 3.5,
      group_id: 2,
      ip_blacklist: ['192.168.1.1'],
      ip_whitelist: ['10.0.0.1', '10.0.0.2'],
      max_concurrency: 4,
      name: 'Production',
      quota_usd: 25.5,
      sell_rate: 1.5,
    }));
    expect(Number.isNaN(Date.parse(onSubmit.mock.calls[0]![0].expires_at))).toBe(false);
  });

  it('blocks balance alert submission until email and threshold are valid', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();

    const { container } = render(
      <CreateKeyModal
        groups={groups}
        loading={false}
        open
        onClose={() => {}}
        onSubmit={onSubmit}
      />,
    );

    await user.type(screen.getByPlaceholderText('api_keys.name_placeholder'), 'Alert Key');
    await user.selectOptions(screen.getByLabelText('api_keys.group'), '1');
    await user.click(screen.getByLabelText('api_keys.balance_alert_enabled'));
    await user.click(screen.getByRole('button', { name: /common\.create/ }));

    expect(mocks.toast).toHaveBeenCalledWith('error', 'api_keys.balance_alert_email_required');
    expect(onSubmit).not.toHaveBeenCalled();

    await user.type(screen.getByPlaceholderText('name@example.com'), 'ops@example.com');
    await user.click(screen.getByRole('button', { name: /common\.create/ }));
    expect(mocks.toast).toHaveBeenLastCalledWith('error', 'api_keys.balance_alert_threshold_required');

    const thresholdInput = container.querySelectorAll<HTMLInputElement>('input[type="number"]')[3]!;
    fireEvent.change(thresholdInput, { target: { value: '1' } });
    await user.click(screen.getByRole('button', { name: /common\.create/ }));
    expect(onSubmit).toHaveBeenCalled();
  });

  it('resets form state when cancelled', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    render(
      <CreateKeyModal
        groups={groups}
        loading={false}
        open
        onClose={onClose}
        onSubmit={() => {}}
      />,
    );

    await user.type(screen.getByPlaceholderText('api_keys.name_placeholder'), 'Temporary');
    await user.click(screen.getByRole('button', { name: /common\.cancel/ }));

    expect(onClose).toHaveBeenCalled();
    expect(screen.getByPlaceholderText('api_keys.name_placeholder')).toHaveValue('');
  });
});
