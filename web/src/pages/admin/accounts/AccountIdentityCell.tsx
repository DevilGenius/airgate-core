import type { ComponentType } from 'react';
import type { PluginPlatformIconProps } from '@devilgenius/airgate-theme/plugin';
import type { AccountResp } from '../../../shared/types';
import { normalizeAccountPlan } from '../../../shared/utils/accountPlan';
import { AccountTypeIcon } from './AccountTypeIcon';
import { AccountPlanTag } from './AccountPlanTag';
import styles from './AccountIdentityCell.module.css';

const PLAN_LABELS: Record<string, string> = {
  free: 'Free', plus: 'Plus', pro: 'Pro', team: 'Team', k12: 'K12',
  prolite: 'ProLite', enterprise: 'Enterprise',
};

// 核心统一表格展示，不依赖已部署插件包是否更新了身份组件。
export function accountIdentityDisplay(
  row: Pick<AccountResp, 'platform' | 'type' | 'credentials'>,
  planOverride?: string,
  now = Date.now(),
) {
  const { credentials } = row;
  const type = row.type === 'api_key' ? 'apikey' : row.type;
  const mode = (credentials.auth_mode || credentials.authMode || '').trim().toLowerCase().replace(/_/g, '');
  const agentIdentity = row.platform === 'openai' && type === 'oauth'
    && (mode === 'agentidentity' || Boolean(credentials.agent_private_key || credentials.agent_runtime_id));
  const typeLabel = agentIdentity ? 'Identity'
    : ({ oauth: 'OAuth', apikey: 'API Key', session_key: 'Session Key' }[type] ?? type);
  const claudeDetail = row.platform === 'claude' || row.platform === 'anthropic'
    ? [credentials.session_key ? '会话' : credentials.access_token ? '令牌' : '',
      credentials.expires_at ? `过期时间：${credentials.expires_at}` : ''].filter(Boolean).join(' · ')
    : '';
  const rawPlan = credentials.plan_type;
  const until = credentials.subscription_active_until;
  const normalized = normalizeAccountPlan(rawPlan);
  const hasQuotaMetadata = row.platform === 'openai' && type === 'oauth'
    && (rawPlan !== undefined || credentials.email !== undefined || until !== undefined);
  const expired = row.platform === 'openai' && (normalized === 'plus' || normalized === 'pro')
    && until !== undefined && Date.parse(until) < now;
  const plan = planOverride || (expired ? 'free' : rawPlan || (hasQuotaMetadata ? 'free' : ''));
  const planKey = normalizeAccountPlan(plan);
  const label = PLAN_LABELS[planKey] ?? (plan ? plan.charAt(0).toUpperCase() + plan.slice(1) : '');
  return {
    type: agentIdentity ? 'identity' : type,
    typeLabel: [typeLabel, claudeDetail].filter(Boolean).join(' · '),
    planLabel: label,
    planKey,
    planTitle: label && planKey !== 'free' && until
      ? `过期时间：${new Date(until).toLocaleDateString()}` : undefined,
  };
}

export function AccountIdentityCell({ row, platformLabel, PlatformGlyph, planOverride }: {
  row: AccountResp;
  platformLabel: string;
  PlatformGlyph: ComponentType<PluginPlatformIconProps>;
  planOverride?: string;
}) {
  const display = accountIdentityDisplay(row, planOverride);
  return (
    <div className={styles.cell}>
      <span className={styles.icon} role="img" title={platformLabel} aria-label={platformLabel}>
        <PlatformGlyph className="size-3.5" />
      </span>
      {display.type ? <AccountTypeIcon type={display.type} label={display.typeLabel} /> : <span aria-hidden="true" />}
      {display.planLabel ? <AccountPlanTag label={display.planLabel} planKey={display.planKey} title={[display.planLabel, display.planTitle].filter(Boolean).join(' · ')} /> : null}
    </div>
  );
}
