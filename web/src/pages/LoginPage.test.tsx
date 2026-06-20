import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import LoginPage from './LoginPage';
import { authApi } from '../shared/api/auth';
import { ApiError, setSessionAPIKey } from '../shared/api/client';
import type { UserResp } from '../shared/types';

const mocks = vi.hoisted(() => ({
  login: vi.fn(),
  navigate: vi.fn(),
  setSessionAPIKey: vi.fn(),
  site: {
    api_base_url: '',
    contact_info: '',
    doc_url: '',
    email_verify_enabled: false,
    frontend_url: '',
    home_content: '',
    registration_enabled: true,
    settings_loaded: true,
    site_logo: '',
    site_name: 'AirGate',
    site_subtitle: 'Control Panel',
  },
  toggleTheme: vi.fn(),
}));

vi.mock('@heroui/react', async () => import('../test/herouiMock'));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock('../app/providers/AuthProvider', () => ({
  useAuth: () => ({
    login: mocks.login,
  }),
}));

vi.mock('../app/providers/SiteSettingsProvider', () => ({
  defaultLogoUrl: '/logo.svg',
  useSiteSettings: () => mocks.site,
}));

vi.mock('../app/providers/ThemeProvider', () => ({
  useTheme: () => ({
    theme: 'dark',
    toggleTheme: mocks.toggleTheme,
  }),
}));

vi.mock('../shared/hooks/useStatusPageEnabled', () => ({
  useStatusPageEnabled: () => false,
}));

vi.mock('../shared/api/auth', () => ({
  authApi: {
    login: vi.fn(),
    loginByAPIKey: vi.fn(),
    register: vi.fn(),
    sendVerifyCode: vi.fn(),
    verifyCode: vi.fn(),
  },
}));

vi.mock('../shared/api/client', () => {
  class MockApiError extends Error {
    constructor(
      public code: number,
      message: string,
      public httpStatus: number,
    ) {
      super(message);
      this.name = 'ApiError';
    }
  }

  return {
    ApiError: MockApiError,
    setSessionAPIKey: mocks.setSessionAPIKey,
  };
});

function userResp(overrides: Partial<UserResp>): UserResp {
  return {
    balance: 0,
    balance_alert_threshold: 0,
    created_at: '2026-06-20T00:00:00Z',
    email: 'user@example.com',
    id: 1,
    max_concurrency: 0,
    role: 'user',
    status: 'active',
    updated_at: '2026-06-20T00:00:00Z',
    username: 'User',
    ...overrides,
  };
}

function lastButton(name: RegExp) {
  const buttons = screen.getAllByRole('button', { name });
  return buttons[buttons.length - 1]!;
}

function loginSubmitButton() {
  return lastButton(/common\.login/);
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.assign(mocks.site, {
      email_verify_enabled: false,
      registration_enabled: true,
      settings_loaded: true,
      site_logo: '',
      site_name: 'AirGate',
    });
  });

  it('submits email/password login and remembers the session when selected', async () => {
    const user = userEvent.setup();
    vi.mocked(authApi.login).mockResolvedValue({
      token: 'jwt-login',
      user: userResp({ email: 'root@example.com', id: 1, role: 'admin', username: 'Root' }),
    });

    render(<LoginPage />);

    await user.type(screen.getByPlaceholderText('auth.email_placeholder'), 'root@example.com');
    await user.type(screen.getByPlaceholderText('auth.password_placeholder'), 'secret');
    await user.click(screen.getByLabelText('auth.remember_login'));
    await user.click(loginSubmitButton());

    await waitFor(() => expect(authApi.login).toHaveBeenCalledWith({
      email: 'root@example.com',
      password: 'secret',
    }));
    expect(mocks.login).toHaveBeenCalledWith(
      'jwt-login',
      userResp({ email: 'root@example.com', id: 1, role: 'admin', username: 'Root' }),
      { remember: true },
    );
    expect(mocks.navigate).toHaveBeenCalledWith({ to: '/' });
  });

  it('renders API errors from email/password login', async () => {
    const user = userEvent.setup();
    vi.mocked(authApi.login).mockRejectedValue(new ApiError(1001, 'invalid credentials', 401));

    render(<LoginPage />);

    await user.type(screen.getByPlaceholderText('auth.email_placeholder'), 'bad@example.com');
    await user.type(screen.getByPlaceholderText('auth.password_placeholder'), 'wrong');
    await user.click(loginSubmitButton());

    expect(await screen.findByText('invalid credentials')).toBeTruthy();
    expect(mocks.login).not.toHaveBeenCalled();
  });

  it('submits API key login, stores the raw key for the session and navigates home', async () => {
    const user = userEvent.setup();
    vi.mocked(authApi.loginByAPIKey).mockResolvedValue({
      api_key_id: 42,
      api_key_name: 'Production Key',
      token: 'jwt-api-key',
      user: userResp({ email: 'key@example.com', id: 2, username: 'Key User' }),
    });

    render(<LoginPage />);

    await user.click(screen.getByRole('tab', { name: 'API Key' }));
    await user.type(screen.getByPlaceholderText('sk-...'), 'sk-live');
    await user.click(screen.getByLabelText('auth.remember_login'));
    await user.click(loginSubmitButton());

    await waitFor(() => expect(authApi.loginByAPIKey).toHaveBeenCalledWith({ key: 'sk-live' }));
    expect(setSessionAPIKey).toHaveBeenCalledWith('sk-live');
    expect(mocks.login).toHaveBeenCalledWith(
      'jwt-api-key',
      expect.objectContaining({
        api_key_id: 42,
        api_key_name: 'Production Key',
        email: 'key@example.com',
      }),
      { remember: true },
    );
    expect(mocks.navigate).toHaveBeenCalledWith({ to: '/' });
  });

  it('registers without email verification and returns to the login tab', async () => {
    const user = userEvent.setup();
    vi.mocked(authApi.register).mockResolvedValue({
      token: 'unused',
      user: userResp({ email: 'new@example.com', id: 3, username: 'New User' }),
    });

    render(<LoginPage />);

    await user.click(screen.getByRole('tab', { name: 'common.register' }));
    await user.type(screen.getByPlaceholderText('auth.email_placeholder'), 'new@example.com');
    await user.click(screen.getByRole('button', { name: /auth\.next_step/ }));
    await user.type(screen.getByPlaceholderText('auth.username_placeholder'), 'New User');
    await user.type(screen.getByPlaceholderText('auth.password_hint'), 'password-1');
    await user.type(screen.getByPlaceholderText('auth.confirm_placeholder'), 'password-1');
    await user.click(lastButton(/common\.register/));

    await waitFor(() => expect(authApi.register).toHaveBeenCalledWith({
      email: 'new@example.com',
      password: 'password-1',
      username: 'New User',
      verify_code: undefined,
    }));
    expect(await screen.findByText('auth.register_success')).toBeTruthy();
  });

  it('requires email verification before completing registration when enabled', async () => {
    const user = userEvent.setup();
    mocks.site.email_verify_enabled = true;
    vi.mocked(authApi.sendVerifyCode).mockResolvedValue(undefined);
    vi.mocked(authApi.verifyCode).mockResolvedValue(undefined);

    render(<LoginPage />);

    await user.click(screen.getByRole('tab', { name: 'common.register' }));
    await user.type(screen.getByPlaceholderText('auth.email_placeholder'), 'verify@example.com');
    await user.click(screen.getByRole('button', { name: 'auth.send_code' }));
    await waitFor(() => expect(authApi.sendVerifyCode).toHaveBeenCalledWith('verify@example.com'));

    await user.type(screen.getByPlaceholderText('auth.verify_code_placeholder'), '123456');
    await user.click(screen.getByRole('button', { name: /auth\.next_step/ }));

    await waitFor(() => expect(authApi.verifyCode).toHaveBeenCalledWith('verify@example.com', '123456'));
    expect(screen.getByText('verify@example.com')).toBeTruthy();
  });
});
