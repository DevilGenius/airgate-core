import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useAuth } from '../../app/providers/AuthProvider';
import { usersApi } from '../../shared/api/users';
import { authApi } from '../../shared/api/auth';
import { useToast } from '../../shared/components/Toast';
import { PageHeader } from '../../shared/components/PageHeader';
import { Card } from '../../shared/components/Card';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Badge } from '../../shared/components/Badge';

export default function ProfilePage() {
  const { toast } = useToast();
  const { user } = useAuth();
  const queryClient = useQueryClient();

  // 修改用户名
  const [username, setUsername] = useState(user?.username || '');
  const profileMutation = useMutation({
    mutationFn: (data: { username: string }) => usersApi.updateProfile(data),
    onSuccess: () => {
      toast('success', '用户名已更新');
      queryClient.invalidateQueries({ queryKey: ['user-me'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 修改密码
  const [passwords, setPasswords] = useState({
    old_password: '',
    new_password: '',
    confirm_password: '',
  });
  const passwordMutation = useMutation({
    mutationFn: (data: { old_password: string; new_password: string }) =>
      usersApi.changePassword(data),
    onSuccess: () => {
      toast('success', '密码已修改');
      setPasswords({ old_password: '', new_password: '', confirm_password: '' });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // TOTP 设置
  const [totpStep, setTotpStep] = useState<'idle' | 'setup' | 'verify'>('idle');
  const [totpUri, setTotpUri] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [disableCode, setDisableCode] = useState('');

  const totpSetupMutation = useMutation({
    mutationFn: () => authApi.totpSetup(),
    onSuccess: (data) => {
      setTotpUri(data.uri);
      setTotpStep('verify');
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const totpVerifyMutation = useMutation({
    mutationFn: (code: string) => authApi.totpVerify({ code }),
    onSuccess: () => {
      toast('success', '双因素认证已启用');
      setTotpStep('idle');
      setTotpCode('');
      setTotpUri('');
      queryClient.invalidateQueries({ queryKey: ['user-me'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const totpDisableMutation = useMutation({
    mutationFn: (code: string) => authApi.totpDisable({ code }),
    onSuccess: () => {
      toast('success', '双因素认证已禁用');
      setDisableCode('');
      queryClient.invalidateQueries({ queryKey: ['user-me'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function handleUpdateUsername() {
    if (!username.trim()) {
      toast('error', '用户名不能为空');
      return;
    }
    profileMutation.mutate({ username: username.trim() });
  }

  function handleChangePassword() {
    if (!passwords.old_password || !passwords.new_password) {
      toast('error', '请填写完整密码信息');
      return;
    }
    if (passwords.new_password !== passwords.confirm_password) {
      toast('error', '两次输入的新密码不一致');
      return;
    }
    if (passwords.new_password.length < 6) {
      toast('error', '新密码至少6个字符');
      return;
    }
    passwordMutation.mutate({
      old_password: passwords.old_password,
      new_password: passwords.new_password,
    });
  }

  if (!user) return null;

  return (
    <div className="p-6 max-w-3xl">
      <PageHeader title="个人资料" />

      {/* 用户信息 */}
      <Card title="基本信息" className="mb-6">
        <div className="space-y-4">
          <div className="flex items-center gap-4">
            <label className="w-24 text-sm font-medium text-gray-700 shrink-0">邮箱</label>
            <span className="text-sm text-gray-900">{user.email}</span>
          </div>
          <div className="flex items-center gap-4">
            <label className="w-24 text-sm font-medium text-gray-700 shrink-0">角色</label>
            <Badge variant={user.role === 'admin' ? 'info' : 'default'}>
              {user.role === 'admin' ? '管理员' : '用户'}
            </Badge>
          </div>
          <div className="flex items-center gap-4">
            <label className="w-24 text-sm font-medium text-gray-700 shrink-0">余额</label>
            <span className="text-sm text-gray-900">${user.balance.toFixed(4)}</span>
          </div>
          <div className="flex items-center gap-4">
            <label className="w-24 text-sm font-medium text-gray-700 shrink-0">并发数</label>
            <span className="text-sm text-gray-900">{user.max_concurrency}</span>
          </div>
        </div>
      </Card>

      {/* 修改用户名 */}
      <Card title="修改用户名" className="mb-6">
        <div className="flex items-end gap-4">
          <div className="flex-1">
            <Input
              label="用户名"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="输入新用户名"
            />
          </div>
          <Button onClick={handleUpdateUsername} loading={profileMutation.isPending}>
            保存
          </Button>
        </div>
      </Card>

      {/* 修改密码 */}
      <Card title="修改密码" className="mb-6">
        <div className="space-y-4">
          <Input
            label="旧密码"
            type="password"
            required
            value={passwords.old_password}
            onChange={(e) =>
              setPasswords({ ...passwords, old_password: e.target.value })
            }
            placeholder="输入当前密码"
          />
          <Input
            label="新密码"
            type="password"
            required
            value={passwords.new_password}
            onChange={(e) =>
              setPasswords({ ...passwords, new_password: e.target.value })
            }
            placeholder="输入新密码（至少6个字符）"
          />
          <Input
            label="确认新密码"
            type="password"
            required
            value={passwords.confirm_password}
            onChange={(e) =>
              setPasswords({ ...passwords, confirm_password: e.target.value })
            }
            placeholder="再次输入新密码"
          />
          <Button onClick={handleChangePassword} loading={passwordMutation.isPending}>
            修改密码
          </Button>
        </div>
      </Card>

      {/* TOTP 双因素认证 */}
      <Card title="双因素认证 (TOTP)">
        {user.totp_enabled ? (
          // 已启用 —— 显示禁用入口
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <Badge variant="success">已启用</Badge>
              <span className="text-sm text-gray-500">双因素认证已开启</span>
            </div>
            <div className="flex items-end gap-4">
              <div className="flex-1">
                <Input
                  label="验证码"
                  value={disableCode}
                  onChange={(e) => setDisableCode(e.target.value)}
                  placeholder="输入 TOTP 验证码以禁用"
                  maxLength={6}
                />
              </div>
              <Button
                variant="danger"
                onClick={() => disableCode && totpDisableMutation.mutate(disableCode)}
                loading={totpDisableMutation.isPending}
              >
                禁用双因素认证
              </Button>
            </div>
          </div>
        ) : (
          // 未启用
          <div className="space-y-4">
            {totpStep === 'idle' && (
              <div>
                <p className="text-sm text-gray-500 mb-3">
                  启用双因素认证可以增强账户安全性。您需要使用 Google Authenticator 或类似的应用扫描二维码。
                </p>
                <Button
                  onClick={() => totpSetupMutation.mutate()}
                  loading={totpSetupMutation.isPending}
                >
                  启用双因素认证
                </Button>
              </div>
            )}

            {totpStep === 'verify' && (
              <div className="space-y-4">
                <p className="text-sm text-gray-500">
                  请使用认证器应用扫描以下 URI 或手动添加：
                </p>
                <div className="bg-gray-50 rounded-md p-3 break-all text-sm font-mono text-gray-700">
                  {totpUri}
                </div>
                <div className="flex items-end gap-4">
                  <div className="flex-1">
                    <Input
                      label="验证码"
                      value={totpCode}
                      onChange={(e) => setTotpCode(e.target.value)}
                      placeholder="输入6位验证码确认绑定"
                      maxLength={6}
                    />
                  </div>
                  <Button
                    onClick={() => totpCode && totpVerifyMutation.mutate(totpCode)}
                    loading={totpVerifyMutation.isPending}
                  >
                    验证并启用
                  </Button>
                  <Button
                    variant="secondary"
                    onClick={() => {
                      setTotpStep('idle');
                      setTotpUri('');
                      setTotpCode('');
                    }}
                  >
                    取消
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}
      </Card>
    </div>
  );
}
