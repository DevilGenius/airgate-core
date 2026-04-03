import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { settingsApi } from '../../shared/api/settings';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { Button } from '../../shared/components/Button';
import { Input, Textarea } from '../../shared/components/Input';
import { Switch } from '../../shared/components/Switch';
import { Card } from '../../shared/components/Card';
import { useToast } from '../../shared/components/Toast';
import {
  Save, Loader2, Globe, UserPlus, Gift, Mail, Send, Upload, X,
} from 'lucide-react';
import type { SettingItem, TestSMTPReq } from '../../shared/types';

// ==================== 设置 key 定义 ====================

const SITE_KEYS = [
  'site_name', 'site_subtitle', 'site_logo', 'api_base_url',
  'contact_info', 'doc_url',
] as const;

const REG_KEYS = [
  'registration_enabled', 'email_verify_enabled',
  'registration_email_suffix_whitelist',
] as const;

const DEFAULT_KEYS = [
  'default_balance', 'default_concurrency',
] as const;

const SMTP_KEYS = [
  'smtp_host', 'smtp_port', 'smtp_username', 'smtp_password',
  'smtp_from_email', 'smtp_from_name', 'smtp_use_tls',
] as const;

// ==================== Tab 定义 ====================

type TabKey = 'site' | 'registration' | 'defaults' | 'smtp';

const TABS: { key: TabKey; labelKey: string; icon: typeof Globe }[] = [
  { key: 'site', labelKey: 'settings.tab_site', icon: Globe },
  { key: 'registration', labelKey: 'settings.tab_registration', icon: UserPlus },
  { key: 'defaults', labelKey: 'settings.tab_defaults', icon: Gift },
  { key: 'smtp', labelKey: 'settings.tab_smtp', icon: Mail },
];

const TAB_GROUP: Record<TabKey, string> = {
  site: 'site',
  registration: 'registration',
  defaults: 'defaults',
  smtp: 'smtp',
};

const TAB_KEYS: Record<TabKey, readonly string[]> = {
  site: SITE_KEYS,
  registration: REG_KEYS,
  defaults: DEFAULT_KEYS,
  smtp: SMTP_KEYS,
};

// ==================== Component ====================

export default function SettingsPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [activeTab, setActiveTab] = useState<TabKey>('site');
  const [values, setValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);

  // 获取所有设置
  const { data: settings, isLoading } = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => settingsApi.list(),
  });

  // 初始化
  useEffect(() => {
    if (settings) {
      const map: Record<string, string> = {};
      for (const s of settings) {
        map[s.key] = s.value;
      }
      setValues(map);
      setHasChanges(false);
    }
  }, [settings]);

  // 保存
  const saveMutation = useCrudMutation({
    mutationFn: (items: SettingItem[]) => settingsApi.update({ settings: items }),
    successMessage: t('settings.save_success'),
    queryKey: queryKeys.settings(),
    onSuccess: () => {
      setHasChanges(false);
      queryClient.invalidateQueries({ queryKey: ['site-settings'] });
    },
  });

  // SMTP 测试
  const smtpTestMutation = useMutation({
    mutationFn: (data: TestSMTPReq) => settingsApi.testSMTP(data),
    onSuccess: () => toast('success', t('settings.smtp_test_success')),
    onError: (err: Error) => toast('error', err.message),
  });

  function set(key: string, value: string) {
    setValues((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  }

  function val(key: string): string {
    return values[key] ?? '';
  }

  function boolVal(key: string): boolean {
    return val(key) === 'true';
  }

  function handleSave() {
    const group = TAB_GROUP[activeTab];
    const keys = TAB_KEYS[activeTab];
    const items: SettingItem[] = keys.map((key) => ({
      key,
      value: values[key] ?? '',
      group,
    }));
    saveMutation.mutate(items);
  }

  function handleTestSMTP() {
    const testTo = prompt(t('settings.smtp_test_prompt'));
    if (!testTo) return;
    smtpTestMutation.mutate({
      host: val('smtp_host'),
      port: Number(val('smtp_port')) || 587,
      username: val('smtp_username'),
      password: val('smtp_password'),
      use_tls: boolVal('smtp_use_tls'),
      from: val('smtp_from_email'),
      to: testTo,
    });
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="w-5 h-5 animate-spin text-primary" />
        <span className="ml-2 text-sm text-text-tertiary">{t('common.loading')}</span>
      </div>
    );
  }

  return (
    <div>
      {/* Tabs */}
      <div className="flex items-center gap-1 mb-6 border-b border-border overflow-x-auto">
        {TABS.map((tab) => {
          const Icon = tab.icon;
          const active = activeTab === tab.key;
          return (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors whitespace-nowrap ${
                active
                  ? 'border-primary text-primary'
                  : 'border-transparent text-text-tertiary hover:text-text-secondary'
              }`}
            >
              <Icon className="w-4 h-4" />
              {t(tab.labelKey)}
            </button>
          );
        })}
      </div>

      {/* Content */}
      <div className="space-y-5">
        {activeTab === 'site' && (
          <Card title={t('settings.site_branding')}>
            <div className="space-y-4">
              <Field label={t('settings.site_name')} hint={t('settings.site_name_hint')}>
                <Input value={val('site_name')} onChange={(e) => set('site_name', e.target.value)} placeholder="AirGate" />
              </Field>
              <Field label={t('settings.site_subtitle')}>
                <Input value={val('site_subtitle')} onChange={(e) => set('site_subtitle', e.target.value)} placeholder="AI API Gateway" />
              </Field>
              <Field label={t('settings.site_logo')} hint={t('settings.site_logo_hint')}>
                <LogoUpload value={val('site_logo')} onChange={(url) => set('site_logo', url)} />
              </Field>
              <Field label={t('settings.api_base_url')} hint={t('settings.api_base_url_hint')}>
                <Input value={val('api_base_url')} onChange={(e) => set('api_base_url', e.target.value)} placeholder="https://api.example.com" />
              </Field>
              <Field label={t('settings.contact_info')}>
                <Input value={val('contact_info')} onChange={(e) => set('contact_info', e.target.value)} />
              </Field>
              <Field label={t('settings.doc_url')}>
                <Input value={val('doc_url')} onChange={(e) => set('doc_url', e.target.value)} placeholder="https://docs.example.com" />
              </Field>
            </div>
          </Card>
        )}

        {activeTab === 'registration' && (
          <Card title={t('settings.registration_auth')}>
            <div className="space-y-5">
              <Switch
                label={t('settings.registration_enabled')}
                description={t('settings.registration_enabled_desc')}
                checked={boolVal('registration_enabled')}
                onChange={(v) => set('registration_enabled', String(v))}
              />
              <Switch
                label={t('settings.email_verify_enabled')}
                description={val('smtp_host') ? t('settings.email_verify_enabled_desc') : t('settings.email_verify_no_smtp')}
                checked={boolVal('email_verify_enabled')}
                onChange={(v) => {
                  if (v && !val('smtp_host')) return;
                  set('email_verify_enabled', String(v));
                }}
                disabled={!val('smtp_host')}
              />
              <Field label={t('settings.email_suffix_whitelist')} hint={t('settings.email_suffix_whitelist_hint')}>
                <Textarea
                  value={val('registration_email_suffix_whitelist')}
                  onChange={(e) => set('registration_email_suffix_whitelist', e.target.value)}
                  rows={3}
                  placeholder="gmail.com&#10;outlook.com"
                />
              </Field>
            </div>
          </Card>
        )}

        {activeTab === 'defaults' && (
          <Card title={t('settings.new_user_defaults')}>
            <div className="space-y-4">
              <Field label={t('settings.default_balance')} hint={t('settings.default_balance_hint')}>
                <Input
                  type="number"
                  value={val('default_balance')}
                  onChange={(e) => set('default_balance', e.target.value)}
                  placeholder="0"
                />
              </Field>
              <Field label={t('settings.default_concurrency')} hint={t('settings.default_concurrency_hint')}>
                <Input
                  type="number"
                  value={val('default_concurrency')}
                  onChange={(e) => set('default_concurrency', e.target.value)}
                  placeholder="5"
                />
              </Field>
            </div>
          </Card>
        )}

        {activeTab === 'smtp' && (
          <Card
            title={t('settings.smtp_config')}
            extra={
              <Button
                size="sm"
                variant="secondary"
                icon={<Send className="w-3.5 h-3.5" />}
                onClick={handleTestSMTP}
                loading={smtpTestMutation.isPending}
                disabled={!val('smtp_host')}
              >
                {t('settings.smtp_test')}
              </Button>
            }
          >
            <div className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <Field label={t('settings.smtp_host')}>
                  <Input value={val('smtp_host')} onChange={(e) => set('smtp_host', e.target.value)} placeholder="smtp.gmail.com" />
                </Field>
                <Field label={t('settings.smtp_port')}>
                  <Input type="number" value={val('smtp_port')} onChange={(e) => set('smtp_port', e.target.value)} placeholder="587" />
                </Field>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <Field label={t('settings.smtp_username')}>
                  <Input value={val('smtp_username')} onChange={(e) => set('smtp_username', e.target.value)} />
                </Field>
                <Field label={t('settings.smtp_password')}>
                  <Input type="password" value={val('smtp_password')} onChange={(e) => set('smtp_password', e.target.value)} />
                </Field>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <Field label={t('settings.smtp_from_email')}>
                  <Input value={val('smtp_from_email')} onChange={(e) => set('smtp_from_email', e.target.value)} placeholder="noreply@example.com" />
                </Field>
                <Field label={t('settings.smtp_from_name')}>
                  <Input value={val('smtp_from_name')} onChange={(e) => set('smtp_from_name', e.target.value)} placeholder="AirGate" />
                </Field>
              </div>
              <Switch
                label={t('settings.smtp_use_tls')}
                description={t('settings.smtp_use_tls_desc')}
                checked={boolVal('smtp_use_tls')}
                onChange={(v) => set('smtp_use_tls', String(v))}
              />
            </div>
          </Card>
        )}
      </div>

      {/* Save button */}
      <div className="flex justify-end mt-6">
        <Button
          icon={<Save className="w-4 h-4" />}
          onClick={handleSave}
          loading={saveMutation.isPending}
          disabled={!hasChanges}
        >
          {t('common.save')}
        </Button>
      </div>
    </div>
  );
}

// ==================== Logo Upload ====================

function LogoUpload({ value, onChange }: { value: string; onChange: (url: string) => void }) {
  const { t } = useTranslation();
  const { toast } = useToast();

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (file.size > 512 * 1024) {
      toast('error', t('settings.logo_too_large'));
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      onChange(reader.result as string);
    };
    reader.readAsDataURL(file);
    e.target.value = '';
  };

  return (
    <div className="flex items-center gap-3">
      {value ? (
        <div className="relative group">
          <img src={value} alt="Logo" className="w-14 h-14 rounded-sm object-cover" />
          <button
            type="button"
            onClick={() => onChange('')}
            className="absolute -top-1.5 -right-1.5 w-5 h-5 rounded-full bg-danger text-white flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      ) : (
        <div className="w-14 h-14 rounded-xl border-2 border-dashed border-glass-border flex items-center justify-center text-text-tertiary">
          <Upload className="w-5 h-5" />
        </div>
      )}
      <label className="cursor-pointer">
        <input type="file" accept="image/png,image/jpeg,image/svg+xml,image/x-icon,image/webp" onChange={handleFile} className="hidden" />
        <span className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg border border-glass-border hover:bg-bg-hover transition-colors">
          <Upload className="w-3.5 h-3.5" />
          {value ? t('settings.change_logo') : t('settings.upload_logo')}
        </span>
      </label>
    </div>
  );
}

// ==================== Field wrapper ====================

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-[13px] font-medium text-text-secondary mb-1.5">
        {label}
      </label>
      {children}
      {hint && <p className="text-[11px] text-text-tertiary mt-1">{hint}</p>}
    </div>
  );
}
