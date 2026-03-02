import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { Button } from '../shared/components/Button';
import { Input } from '../shared/components/Input';
import { Card } from '../shared/components/Card';
import { setupApi } from '../shared/api/setup';
import type { TestDBReq, TestRedisReq, AdminSetup } from '../shared/types';

// ==================== 步骤指示器 ====================

const STEPS = ['数据库配置', 'Redis 配置', '管理员账户', '完成安装'] as const;

function Stepper({ current }: { current: number }) {
  return (
    <div className="flex items-center justify-center mb-8">
      {STEPS.map((label, index) => (
        <div key={label} className="flex items-center">
          {/* 步骤圆圈 */}
          <div className="flex flex-col items-center">
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center text-sm font-medium transition-colors ${
                index < current
                  ? 'bg-indigo-600 text-white'
                  : index === current
                    ? 'bg-indigo-600 text-white ring-4 ring-indigo-100'
                    : 'bg-gray-200 text-gray-500'
              }`}
            >
              {index < current ? '✓' : index + 1}
            </div>
            <span
              className={`text-xs mt-1.5 whitespace-nowrap ${
                index <= current ? 'text-indigo-600 font-medium' : 'text-gray-400'
              }`}
            >
              {label}
            </span>
          </div>
          {/* 连接线 */}
          {index < STEPS.length - 1 && (
            <div
              className={`w-16 h-0.5 mx-2 mb-5 ${
                index < current ? 'bg-indigo-600' : 'bg-gray-200'
              }`}
            />
          )}
        </div>
      ))}
    </div>
  );
}

// ==================== Step 1: 数据库配置 ====================

interface DBStepProps {
  data: TestDBReq;
  onChange: (data: TestDBReq) => void;
  onNext: () => void;
}

function DBStep({ data, onChange, onNext }: DBStepProps) {
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; error_msg?: string } | null>(null);

  const update = (field: keyof TestDBReq, value: string | number) => {
    onChange({ ...data, [field]: value });
    setTestResult(null);
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const result = await setupApi.testDB(data);
      setTestResult(result);
    } catch (err) {
      setTestResult({ success: false, error_msg: err instanceof Error ? err.message : '连接失败' });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-500 mb-4">配置 PostgreSQL 数据库连接信息</p>
      <div className="grid grid-cols-2 gap-4">
        <Input
          label="主机地址"
          value={data.host}
          onChange={(e) => update('host', e.target.value)}
          placeholder="localhost"
          required
        />
        <Input
          label="端口"
          type="number"
          value={data.port}
          onChange={(e) => update('port', Number(e.target.value))}
          placeholder="5432"
          required
        />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <Input
          label="用户名"
          value={data.user}
          onChange={(e) => update('user', e.target.value)}
          placeholder="postgres"
          required
        />
        <Input
          label="密码"
          type="password"
          value={data.password || ''}
          onChange={(e) => update('password', e.target.value)}
          placeholder="数据库密码"
        />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <Input
          label="数据库名"
          value={data.dbname}
          onChange={(e) => update('dbname', e.target.value)}
          placeholder="airgate"
          required
        />
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">SSL 模式</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
            value={data.sslmode || 'disable'}
            onChange={(e) => update('sslmode', e.target.value)}
          >
            <option value="disable">disable</option>
            <option value="require">require</option>
            <option value="verify-ca">verify-ca</option>
            <option value="verify-full">verify-full</option>
          </select>
        </div>
      </div>

      {/* 测试结果 */}
      {testResult && (
        <div
          className={`rounded-md p-3 text-sm ${
            testResult.success
              ? 'bg-green-50 text-green-700 border border-green-200'
              : 'bg-red-50 text-red-700 border border-red-200'
          }`}
        >
          {testResult.success ? '连接成功' : `连接失败：${testResult.error_msg}`}
        </div>
      )}

      {/* 操作按钮 */}
      <div className="flex justify-between pt-4">
        <Button variant="secondary" onClick={handleTest} loading={testing}>
          测试连接
        </Button>
        <Button onClick={onNext} disabled={!testResult?.success}>
          下一步
        </Button>
      </div>
    </div>
  );
}

// ==================== Step 2: Redis 配置 ====================

interface RedisStepProps {
  data: TestRedisReq;
  onChange: (data: TestRedisReq) => void;
  onPrev: () => void;
  onNext: () => void;
}

function RedisStep({ data, onChange, onPrev, onNext }: RedisStepProps) {
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; error_msg?: string } | null>(null);

  const update = (field: keyof TestRedisReq, value: string | number | boolean) => {
    onChange({ ...data, [field]: value });
    setTestResult(null);
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const result = await setupApi.testRedis(data);
      setTestResult(result);
    } catch (err) {
      setTestResult({ success: false, error_msg: err instanceof Error ? err.message : '连接失败' });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-500 mb-4">配置 Redis 缓存连接信息</p>
      <div className="grid grid-cols-2 gap-4">
        <Input
          label="主机地址"
          value={data.host}
          onChange={(e) => update('host', e.target.value)}
          placeholder="localhost"
          required
        />
        <Input
          label="端口"
          type="number"
          value={data.port}
          onChange={(e) => update('port', Number(e.target.value))}
          placeholder="6379"
          required
        />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <Input
          label="密码"
          type="password"
          value={data.password || ''}
          onChange={(e) => update('password', e.target.value)}
          placeholder="留空表示无密码"
        />
        <Input
          label="数据库编号"
          type="number"
          value={data.db ?? 0}
          onChange={(e) => update('db', Number(e.target.value))}
          placeholder="0"
        />
      </div>
      <div className="flex items-center gap-2">
        <input
          type="checkbox"
          id="redis-tls"
          checked={data.tls || false}
          onChange={(e) => update('tls', e.target.checked)}
          className="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
        />
        <label htmlFor="redis-tls" className="text-sm text-gray-700">
          启用 TLS 加密连接
        </label>
      </div>

      {/* 测试结果 */}
      {testResult && (
        <div
          className={`rounded-md p-3 text-sm ${
            testResult.success
              ? 'bg-green-50 text-green-700 border border-green-200'
              : 'bg-red-50 text-red-700 border border-red-200'
          }`}
        >
          {testResult.success ? '连接成功' : `连接失败：${testResult.error_msg}`}
        </div>
      )}

      {/* 操作按钮 */}
      <div className="flex justify-between pt-4">
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onPrev}>
            上一步
          </Button>
          <Button variant="secondary" onClick={handleTest} loading={testing}>
            测试连接
          </Button>
        </div>
        <Button onClick={onNext} disabled={!testResult?.success}>
          下一步
        </Button>
      </div>
    </div>
  );
}

// ==================== Step 3: 管理员账户 ====================

interface AdminStepProps {
  data: AdminSetup & { confirmPassword: string };
  onChange: (data: AdminSetup & { confirmPassword: string }) => void;
  onPrev: () => void;
  onNext: () => void;
}

function AdminStep({ data, onChange, onPrev, onNext }: AdminStepProps) {
  const update = (field: string, value: string) => {
    onChange({ ...data, [field]: value });
  };

  // 密码强度检查
  const getPasswordStrength = (pwd: string): { label: string; color: string } => {
    if (pwd.length < 6) return { label: '太短', color: 'text-red-500' };
    if (pwd.length < 8) return { label: '弱', color: 'text-yellow-500' };
    const hasUpper = /[A-Z]/.test(pwd);
    const hasLower = /[a-z]/.test(pwd);
    const hasNumber = /\d/.test(pwd);
    const hasSpecial = /[^A-Za-z0-9]/.test(pwd);
    const score = [hasUpper, hasLower, hasNumber, hasSpecial].filter(Boolean).length;
    if (score >= 3 && pwd.length >= 10) return { label: '强', color: 'text-green-600' };
    if (score >= 2) return { label: '中等', color: 'text-yellow-500' };
    return { label: '弱', color: 'text-red-500' };
  };

  const passwordMismatch = data.confirmPassword && data.password !== data.confirmPassword;
  const passwordTooShort = data.password.length > 0 && data.password.length < 8;
  const strength = data.password ? getPasswordStrength(data.password) : null;

  const canProceed =
    data.email.trim() !== '' &&
    data.password.length >= 8 &&
    data.password === data.confirmPassword;

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-500 mb-4">创建系统管理员账户</p>
      <Input
        label="管理员邮箱"
        type="email"
        value={data.email}
        onChange={(e) => update('email', e.target.value)}
        placeholder="admin@example.com"
        required
      />
      <div>
        <Input
          label="密码"
          type="password"
          value={data.password}
          onChange={(e) => update('password', e.target.value)}
          placeholder="至少 8 个字符"
          required
          error={passwordTooShort ? '密码至少需要 8 个字符' : undefined}
        />
        {strength && !passwordTooShort && (
          <p className={`text-xs mt-1 ${strength.color}`}>密码强度：{strength.label}</p>
        )}
      </div>
      <Input
        label="确认密码"
        type="password"
        value={data.confirmPassword}
        onChange={(e) => update('confirmPassword', e.target.value)}
        placeholder="再次输入密码"
        required
        error={passwordMismatch ? '两次输入的密码不一致' : undefined}
      />

      {/* 操作按钮 */}
      <div className="flex justify-between pt-4">
        <Button variant="ghost" onClick={onPrev}>
          上一步
        </Button>
        <Button onClick={onNext} disabled={!canProceed}>
          下一步
        </Button>
      </div>
    </div>
  );
}

// ==================== Step 4: 完成安装 ====================

interface FinishStepProps {
  dbConfig: TestDBReq;
  redisConfig: TestRedisReq;
  adminConfig: AdminSetup;
  onPrev: () => void;
}

function FinishStep({ dbConfig, redisConfig, adminConfig, onPrev }: FinishStepProps) {
  const navigate = useNavigate();
  const [installing, setInstalling] = useState(false);
  const [status, setStatus] = useState<'idle' | 'installing' | 'restarting' | 'done' | 'error'>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  // 轮询服务状态，等待重启完成
  const pollStatus = () => {
    setStatus('restarting');
    const maxAttempts = 30;
    let attempt = 0;

    const poll = () => {
      attempt++;
      setupApi
        .status()
        .then((resp) => {
          if (!resp.needs_setup) {
            setStatus('done');
            setTimeout(() => navigate({ to: '/login' }), 1500);
          } else if (attempt < maxAttempts) {
            setTimeout(poll, 2000);
          } else {
            setStatus('done');
            setTimeout(() => navigate({ to: '/login' }), 1500);
          }
        })
        .catch(() => {
          // 服务可能还在重启，继续轮询
          if (attempt < maxAttempts) {
            setTimeout(poll, 2000);
          } else {
            setStatus('done');
            setTimeout(() => navigate({ to: '/login' }), 1500);
          }
        });
    };

    setTimeout(poll, 3000);
  };

  const handleInstall = async () => {
    setInstalling(true);
    setStatus('installing');
    setErrorMsg('');
    try {
      await setupApi.install({
        database: dbConfig,
        redis: redisConfig,
        admin: adminConfig,
      });
      pollStatus();
    } catch (err) {
      setStatus('error');
      setErrorMsg(err instanceof Error ? err.message : '安装失败');
      setInstalling(false);
    }
  };

  return (
    <div className="space-y-6">
      <p className="text-sm text-gray-500 mb-4">请确认以下配置信息，然后执行安装</p>

      {/* 配置摘要 */}
      <div className="space-y-4">
        {/* 数据库摘要 */}
        <div className="rounded-md border border-gray-200 p-4">
          <h4 className="text-sm font-medium text-gray-900 mb-2">数据库</h4>
          <div className="grid grid-cols-2 gap-2 text-sm text-gray-600">
            <span>主机：{dbConfig.host}:{dbConfig.port}</span>
            <span>用户：{dbConfig.user}</span>
            <span>数据库：{dbConfig.dbname}</span>
            <span>SSL：{dbConfig.sslmode || 'disable'}</span>
          </div>
        </div>

        {/* Redis 摘要 */}
        <div className="rounded-md border border-gray-200 p-4">
          <h4 className="text-sm font-medium text-gray-900 mb-2">Redis</h4>
          <div className="grid grid-cols-2 gap-2 text-sm text-gray-600">
            <span>主机：{redisConfig.host}:{redisConfig.port}</span>
            <span>数据库：{redisConfig.db ?? 0}</span>
            <span>密码：{redisConfig.password ? '******' : '无'}</span>
            <span>TLS：{redisConfig.tls ? '已启用' : '未启用'}</span>
          </div>
        </div>

        {/* 管理员摘要 */}
        <div className="rounded-md border border-gray-200 p-4">
          <h4 className="text-sm font-medium text-gray-900 mb-2">管理员</h4>
          <div className="text-sm text-gray-600">
            <span>邮箱：{adminConfig.email}</span>
          </div>
        </div>
      </div>

      {/* 安装状态 */}
      {status === 'installing' && (
        <div className="rounded-md bg-blue-50 border border-blue-200 p-3 text-sm text-blue-700">
          正在安装，请稍候...
        </div>
      )}
      {status === 'restarting' && (
        <div className="rounded-md bg-yellow-50 border border-yellow-200 p-3 text-sm text-yellow-700">
          安装成功！正在等待服务重启...
        </div>
      )}
      {status === 'done' && (
        <div className="rounded-md bg-green-50 border border-green-200 p-3 text-sm text-green-700">
          安装完成！即将跳转到登录页面...
        </div>
      )}
      {status === 'error' && (
        <div className="rounded-md bg-red-50 border border-red-200 p-3 text-sm text-red-700">
          安装失败：{errorMsg}
        </div>
      )}

      {/* 操作按钮 */}
      <div className="flex justify-between pt-4">
        <Button variant="ghost" onClick={onPrev} disabled={installing}>
          上一步
        </Button>
        <Button onClick={handleInstall} loading={installing} disabled={status === 'done'}>
          {status === 'idle' || status === 'error' ? '执行安装' : '安装中...'}
        </Button>
      </div>
    </div>
  );
}

// ==================== 安装向导主页面 ====================

export default function SetupPage() {
  const [step, setStep] = useState(0);

  // 各步骤的表单数据
  const [dbConfig, setDBConfig] = useState<TestDBReq>({
    host: 'localhost',
    port: 5432,
    user: 'postgres',
    password: '',
    dbname: 'airgate',
    sslmode: 'disable',
  });

  const [redisConfig, setRedisConfig] = useState<TestRedisReq>({
    host: 'localhost',
    port: 6379,
    password: '',
    db: 0,
    tls: false,
  });

  const [adminConfig, setAdminConfig] = useState<AdminSetup & { confirmPassword: string }>({
    email: '',
    password: '',
    confirmPassword: '',
  });

  return (
    <div className="min-h-screen bg-gray-50 flex items-center justify-center p-4">
      <div className="w-full max-w-xl">
        {/* 标题 */}
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-gray-900">AirGate</h1>
          <p className="text-gray-500 mt-2">系统安装向导</p>
        </div>

        {/* 步骤指示器 */}
        <Stepper current={step} />

        {/* 表单卡片 */}
        <Card>
          {step === 0 && (
            <DBStep data={dbConfig} onChange={setDBConfig} onNext={() => setStep(1)} />
          )}
          {step === 1 && (
            <RedisStep
              data={redisConfig}
              onChange={setRedisConfig}
              onPrev={() => setStep(0)}
              onNext={() => setStep(2)}
            />
          )}
          {step === 2 && (
            <AdminStep
              data={adminConfig}
              onChange={setAdminConfig}
              onPrev={() => setStep(1)}
              onNext={() => setStep(3)}
            />
          )}
          {step === 3 && (
            <FinishStep
              dbConfig={dbConfig}
              redisConfig={redisConfig}
              adminConfig={{ email: adminConfig.email, password: adminConfig.password }}
              onPrev={() => setStep(2)}
            />
          )}
        </Card>
      </div>
    </div>
  );
}
