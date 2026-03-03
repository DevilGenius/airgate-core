import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Button } from '../shared/components/Button';
import { Input } from '../shared/components/Input';
import { useAuth } from '../app/providers/AuthProvider';
import { authApi } from '../shared/api/auth';
import { ApiError } from '../shared/api/client';
import { Mail, Lock, User, Zap, ShieldCheck } from 'lucide-react';

type TabKey = 'login' | 'register';

/* ==================== 登录表单 ==================== */

function LoginForm() {
  const navigate = useNavigate();
  const { login } = useAuth();
  const { t } = useTranslation();

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [needsTotp, setNeedsTotp] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');

    try {
      const resp = await authApi.login({
        email,
        password,
        totp_code: needsTotp ? totpCode : undefined,
      });
      login(resp.token, resp.user);
      navigate({ to: '/' });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.message.toLowerCase().includes('totp') || err.code === 40102) {
          setNeedsTotp(true);
          setError(t('auth.totp_required'));
        } else {
          setError(err.message);
        }
      } else {
        setError(t('auth.login_failed'));
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-5">
      <Input
        label={t('auth.email')}
        type="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        placeholder={t('auth.email_placeholder')}
        icon={<Mail className="w-4 h-4" />}
        required
        autoFocus
      />
      <Input
        label={t('auth.password')}
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder={t('auth.password_placeholder')}
        icon={<Lock className="w-4 h-4" />}
        required
      />
      {needsTotp && (
        <Input
          label={t('auth.totp_label')}
          value={totpCode}
          onChange={(e) => setTotpCode(e.target.value)}
          placeholder={t('auth.totp_placeholder')}
          icon={<ShieldCheck className="w-4 h-4" />}
          maxLength={6}
          autoFocus
          required
        />
      )}
      {error && (
        <div className="rounded-[var(--ag-radius-md)] bg-[var(--ag-danger-subtle)] border border-[var(--ag-danger)] border-opacity-20 px-4 py-3 text-sm text-[var(--ag-danger)]">
          {error}
        </div>
      )}
      <Button type="submit" loading={loading} className="w-full h-11">
        {t('common.login')}
      </Button>
    </form>
  );
}

/* ==================== 注册表单 ==================== */

function RegisterForm({ onSuccess }: { onSuccess: () => void }) {
  const { t } = useTranslation();

  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const passwordMismatch = confirmPassword !== '' && password !== confirmPassword;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (password !== confirmPassword) {
      setError(t('auth.password_mismatch'));
      return;
    }
    if (password.length < 8) {
      setError(t('auth.password_too_short'));
      return;
    }

    setLoading(true);
    setError('');

    try {
      await authApi.register({ email, password, username: username || undefined });
      onSuccess();
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t('auth.register_failed'));
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-5">
      <Input
        label={t('auth.email')}
        type="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        placeholder={t('auth.email_placeholder')}
        icon={<Mail className="w-4 h-4" />}
        required
        autoFocus
      />
      <Input
        label={t('auth.username')}
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        placeholder={t('auth.username_placeholder')}
        icon={<User className="w-4 h-4" />}
      />
      <Input
        label={t('auth.password')}
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder={t('auth.password_hint')}
        icon={<Lock className="w-4 h-4" />}
        required
      />
      <Input
        label={t('auth.confirm_password')}
        type="password"
        value={confirmPassword}
        onChange={(e) => setConfirmPassword(e.target.value)}
        placeholder={t('auth.confirm_placeholder')}
        icon={<Lock className="w-4 h-4" />}
        required
        error={passwordMismatch ? t('auth.password_mismatch') : undefined}
      />
      {error && (
        <div className="rounded-[var(--ag-radius-md)] bg-[var(--ag-danger-subtle)] border border-[var(--ag-danger)] border-opacity-20 px-4 py-3 text-sm text-[var(--ag-danger)]">
          {error}
        </div>
      )}
      <Button type="submit" loading={loading} className="w-full h-11">
        {t('common.register')}
      </Button>
    </form>
  );
}

/* ==================== 登录页主组件 ==================== */

export default function LoginPage() {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TabKey>('login');
  const [registerSuccess, setRegisterSuccess] = useState(false);

  const handleRegisterSuccess = () => {
    setRegisterSuccess(true);
    setActiveTab('login');
  };

  return (
    <div className="min-h-screen bg-[var(--ag-bg-deep)] flex items-center justify-center p-4 relative overflow-hidden">
      {/* 背景装饰 */}
      <div className="absolute inset-0">
        {/* 渐变光晕 */}
        <div className="absolute top-[-20%] left-[-10%] w-[600px] h-[600px] rounded-full opacity-[0.07]"
          style={{ background: 'radial-gradient(circle, var(--ag-primary), transparent 70%)' }}
        />
        <div className="absolute bottom-[-20%] right-[-10%] w-[500px] h-[500px] rounded-full opacity-[0.05]"
          style={{ background: 'radial-gradient(circle, var(--ag-info), transparent 70%)' }}
        />
        {/* 网格纹理 */}
        <div className="absolute inset-0 opacity-[0.03]"
          style={{
            backgroundImage: `linear-gradient(var(--ag-text) 1px, transparent 1px), linear-gradient(90deg, var(--ag-text) 1px, transparent 1px)`,
            backgroundSize: '60px 60px',
          }}
        />
      </div>

      <div className="relative w-full max-w-[420px]" style={{ animation: 'ag-slide-up 0.5s ease-out' }}>
        {/* Logo */}
        <div className="text-center mb-10">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-[var(--ag-radius-xl)] bg-[var(--ag-primary-subtle)] mb-4 shadow-[var(--ag-shadow-glow)]">
            <Zap className="w-7 h-7 text-[var(--ag-primary)]" />
          </div>
          <h1 className="text-2xl font-bold text-[var(--ag-text)] tracking-tight">
            {t('app_name')}
          </h1>
          <p className="text-sm text-[var(--ag-text-tertiary)] mt-1.5 tracking-wide">
            {t('app_subtitle')}
          </p>
        </div>

        {/* 表单卡片 */}
        <div className="rounded-[var(--ag-radius-xl)] border border-[var(--ag-glass-border)] bg-[var(--ag-bg-elevated)] shadow-[var(--ag-shadow-lg)] overflow-hidden">
          {/* Tab 切换 */}
          <div className="flex border-b border-[var(--ag-border)]">
            {(['login', 'register'] as const).map((tab) => (
              <button
                key={tab}
                className={`flex-1 py-3.5 text-sm font-medium text-center transition-all relative ${
                  activeTab === tab
                    ? 'text-[var(--ag-primary)]'
                    : 'text-[var(--ag-text-tertiary)] hover:text-[var(--ag-text-secondary)]'
                }`}
                onClick={() => {
                  setActiveTab(tab);
                  setRegisterSuccess(false);
                }}
              >
                {tab === 'login' ? t('common.login') : t('common.register')}
                {activeTab === tab && (
                  <div className="absolute bottom-0 left-1/4 right-1/4 h-[2px] bg-[var(--ag-primary)] rounded-full" />
                )}
              </button>
            ))}
          </div>

          <div className="p-6">
            {/* 注册成功提示 */}
            {registerSuccess && activeTab === 'login' && (
              <div className="rounded-[var(--ag-radius-md)] bg-[var(--ag-success-subtle)] border border-[var(--ag-success)] border-opacity-20 px-4 py-3 text-sm text-[var(--ag-success)] mb-5">
                {t('auth.register_success')}
              </div>
            )}

            {activeTab === 'login' ? (
              <LoginForm />
            ) : (
              <RegisterForm onSuccess={handleRegisterSuccess} />
            )}
          </div>
        </div>

        {/* 底部文字 */}
        <p className="text-center text-[10px] text-[var(--ag-text-tertiary)] mt-6 uppercase tracking-widest">
          Powered by AirGate
        </p>
      </div>
    </div>
  );
}
