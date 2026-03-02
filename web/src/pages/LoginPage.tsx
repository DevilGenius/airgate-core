import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { Button } from '../shared/components/Button';
import { Input } from '../shared/components/Input';
import { Card } from '../shared/components/Card';
import { useAuth } from '../app/providers/AuthProvider';
import { authApi } from '../shared/api/auth';
import { ApiError } from '../shared/api/client';

type TabKey = 'login' | 'register';

// ==================== 登录表单 ====================

function LoginForm() {
  const navigate = useNavigate();
  const { login } = useAuth();

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
        // 后端返回需要 TOTP 验证的错误码
        if (err.message.toLowerCase().includes('totp') || err.code === 40102) {
          setNeedsTotp(true);
          setError('请输入两步验证码');
        } else {
          setError(err.message);
        }
      } else {
        setError('登录失败，请稍后重试');
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input
        label="邮箱"
        type="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        placeholder="your@email.com"
        required
        autoFocus
      />
      <Input
        label="密码"
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder="输入密码"
        required
      />
      {needsTotp && (
        <Input
          label="两步验证码"
          value={totpCode}
          onChange={(e) => setTotpCode(e.target.value)}
          placeholder="6 位验证码"
          maxLength={6}
          autoFocus
          required
        />
      )}
      {error && (
        <div className="rounded-md bg-red-50 border border-red-200 p-3 text-sm text-red-700">
          {error}
        </div>
      )}
      <Button type="submit" loading={loading} className="w-full">
        登录
      </Button>
    </form>
  );
}

// ==================== 注册表单 ====================

interface RegisterFormProps {
  onSuccess: () => void;
}

function RegisterForm({ onSuccess }: RegisterFormProps) {
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
      setError('两次输入的密码不一致');
      return;
    }
    if (password.length < 8) {
      setError('密码至少需要 8 个字符');
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
        setError('注册失败，请稍后重试');
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input
        label="邮箱"
        type="email"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        placeholder="your@email.com"
        required
        autoFocus
      />
      <Input
        label="用户名"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        placeholder="可选"
      />
      <Input
        label="密码"
        type="password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        placeholder="至少 8 个字符"
        required
      />
      <Input
        label="确认密码"
        type="password"
        value={confirmPassword}
        onChange={(e) => setConfirmPassword(e.target.value)}
        placeholder="再次输入密码"
        required
        error={passwordMismatch ? '两次输入的密码不一致' : undefined}
      />
      {error && (
        <div className="rounded-md bg-red-50 border border-red-200 p-3 text-sm text-red-700">
          {error}
        </div>
      )}
      <Button type="submit" loading={loading} className="w-full">
        注册
      </Button>
    </form>
  );
}

// ==================== 登录页主组件 ====================

export default function LoginPage() {
  const [activeTab, setActiveTab] = useState<TabKey>('login');
  const [registerSuccess, setRegisterSuccess] = useState(false);

  const handleRegisterSuccess = () => {
    setRegisterSuccess(true);
    setActiveTab('login');
  };

  return (
    <div className="min-h-screen bg-gray-50 flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        {/* 标题 */}
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-gray-900">AirGate</h1>
          <p className="text-gray-500 mt-2">API 网关管理平台</p>
        </div>

        <Card>
          {/* Tab 切换 */}
          <div className="flex border-b border-gray-200 -mx-6 -mt-6 mb-6">
            <button
              className={`flex-1 py-3 text-sm font-medium text-center transition-colors ${
                activeTab === 'login'
                  ? 'text-indigo-600 border-b-2 border-indigo-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
              onClick={() => {
                setActiveTab('login');
                setRegisterSuccess(false);
              }}
            >
              登录
            </button>
            <button
              className={`flex-1 py-3 text-sm font-medium text-center transition-colors ${
                activeTab === 'register'
                  ? 'text-indigo-600 border-b-2 border-indigo-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
              onClick={() => {
                setActiveTab('register');
                setRegisterSuccess(false);
              }}
            >
              注册
            </button>
          </div>

          {/* 注册成功提示 */}
          {registerSuccess && activeTab === 'login' && (
            <div className="rounded-md bg-green-50 border border-green-200 p-3 text-sm text-green-700 mb-4">
              注册成功！请使用邮箱和密码登录。
            </div>
          )}

          {/* 表单内容 */}
          {activeTab === 'login' ? (
            <LoginForm />
          ) : (
            <RegisterForm onSuccess={handleRegisterSuccess} />
          )}
        </Card>
      </div>
    </div>
  );
}
