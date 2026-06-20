import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CreateKeyModal } from './CreateKeyModal';

const mocks = vi.hoisted(() => ({
  copy: vi.fn(),
}));

vi.mock('@heroui/react', async () => import('../../../test/herouiMock'));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

vi.mock('../../../shared/hooks/useClipboard', () => ({
  useClipboard: () => mocks.copy,
}));

vi.mock('../../../shared/components/DialogTriggerShim', () => ({
  DialogTriggerShim: () => null,
}));

describe('user CreateKeyModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the created key once and copies it on demand', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    render(<CreateKeyModal createdKey="sk-created" open onClose={onClose} />);

    expect(screen.getByText('user_keys.create_success')).toBeTruthy();
    expect(screen.getByText('sk-created')).toBeTruthy();

    await user.click(screen.getByRole('button', { name: /user_keys\.copy_key/ }));
    expect(mocks.copy).toHaveBeenCalledWith('sk-created', 'user_keys.copy_key');

    await user.click(screen.getByRole('button', { name: /user_keys\.key_saved_close/ }));
    expect(onClose).toHaveBeenCalled();
  });

  it('does not render body when closed', () => {
    render(<CreateKeyModal createdKey="sk-created" open={false} onClose={() => {}} />);

    expect(screen.queryByText('sk-created')).toBeNull();
  });
});
