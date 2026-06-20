import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  CredentialFieldInput,
  GroupCheckboxList,
  SchemaCredentialsForm,
} from './CredentialForm';
import type { CredentialSchemaResp } from '../../../shared/types';

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

vi.mock('../../../shared/components/SimpleSelect', () => ({
  SimpleSelect: ({
    ariaLabel,
    items,
    onSelectionChange,
    selectedKey,
  }: {
    ariaLabel: string;
    items: Array<{ key: string; label: string }>;
    onSelectionChange: (key: string) => void;
    selectedKey?: string;
  }) => (
    <select
      aria-label={ariaLabel}
      value={selectedKey ?? ''}
      onChange={(event) => onSelectionChange(event.currentTarget.value)}
    >
      {items.map((item) => (
        <option key={item.key} value={item.key}>{item.label}</option>
      ))}
    </select>
  ),
}));

const schema: CredentialSchemaResp = {
  account_types: [
    {
      description: 'Use a static API key.',
      fields: [
        { key: 'api_key', label: 'API Key', placeholder: 'sk-...', required: true, type: 'password' },
        { key: 'note', label: 'Note', placeholder: '', required: false, type: 'textarea' },
      ],
      key: 'apikey',
      label: 'API Key',
    },
    {
      description: '',
      fields: [
        { edit_disabled: true, key: 'client_secret', label: 'Client Secret', placeholder: '', required: false, type: 'password' },
        { key: 'refresh_token', label: 'Refresh Token', placeholder: '', required: false, type: 'textarea' },
      ],
      key: 'oauth',
      label: 'OAuth',
    },
  ],
  fields: [],
};

describe('CredentialFieldInput', () => {
  it('renders text-like fields and propagates changes', async () => {
    const onChange = vi.fn();

    render(
      <CredentialFieldInput
        field={{ key: 'api_key', label: 'API Key', placeholder: '', required: true, type: 'password' }}
        value="old"
        onChange={onChange}
      />,
    );

    const input = screen.getByDisplayValue('old') as HTMLInputElement;
    expect(input.type).toBe('text');
    expect(input.name).toBe('api_key');
    fireEvent.change(input, { target: { value: 'new-key' } });

    expect(onChange).toHaveBeenLastCalledWith('new-key');
  });

  it('renders textarea fields with placeholders', async () => {
    const onChange = vi.fn();

    render(
      <CredentialFieldInput
        field={{ key: 'session', label: 'Session', placeholder: 'paste session', required: false, type: 'textarea' }}
        value=""
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText('paste session'), { target: { value: 'cookie-value' } });
    expect(onChange).toHaveBeenLastCalledWith('cookie-value');
  });
});

describe('SchemaCredentialsForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders account type selector and selected type fields in create mode', async () => {
    const user = userEvent.setup();
    const onAccountTypeChange = vi.fn();
    const onCredentialsChange = vi.fn();

    render(
      <SchemaCredentialsForm
        accountType="apikey"
        credentials={{ api_key: 'sk-old' }}
        schema={schema}
        onAccountTypeChange={onAccountTypeChange}
        onCredentialsChange={onCredentialsChange}
      />,
    );

    expect(screen.getByText('Use a static API key.')).toBeTruthy();
    expect(screen.getByDisplayValue('sk-old')).toBeTruthy();

    await user.selectOptions(screen.getByLabelText('common.type'), 'oauth');
    expect(onAccountTypeChange).toHaveBeenCalledWith('oauth');

    fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'sk-old-edited' } });
    expect(onCredentialsChange).toHaveBeenLastCalledWith({ api_key: 'sk-old-edited' });
  });

  it('hides edit-disabled fields and uses password keep placeholder in edit mode', () => {
    render(
      <SchemaCredentialsForm
        accountType="oauth"
        credentials={{ client_secret: 'secret', refresh_token: 'refresh' }}
        mode="edit"
        schema={schema}
        onAccountTypeChange={() => {}}
        onCredentialsChange={() => {}}
      />,
    );

    expect(screen.queryByDisplayValue('secret')).toBeNull();
    expect(screen.getByDisplayValue('refresh')).toBeTruthy();
  });
});

describe('GroupCheckboxList', () => {
  it('toggles group selections and closes on Escape', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();

    render(
      <GroupCheckboxList
        groups={[
          { id: 1, name: 'Primary', platform: 'openai' },
          { id: 2, name: 'Backup', platform: 'claude' },
        ]}
        selectedIds={[1]}
        onChange={onChange}
      />,
    );

    await user.click(screen.getByRole('button', { name: 'accounts.groups' }));
    expect(screen.getByRole('option', { name: /Primary/ })).toBeTruthy();

    await user.click(screen.getByRole('option', { name: /Backup/ }));
    expect(onChange).toHaveBeenCalledWith([1, 2]);

    await user.keyboard('{Escape}');
    expect(screen.queryByRole('option', { name: /Primary/ })).toBeNull();
  });

  it('renders nothing when no groups are available', () => {
    const { container } = render(
      <GroupCheckboxList groups={[]} selectedIds={[]} onChange={() => {}} />,
    );

    expect(container.firstChild).toBeNull();
  });
});
