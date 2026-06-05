import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../providers/AuthProvider';
import { useSiteSettings } from '../providers/SiteSettingsProvider';
import { getTokenRole } from '../../shared/api/client';
import type { UserResp } from '../../shared/types';

export interface ShellIdentity {
  balanceText: string;
  balanceValue: number | null;
  displayName: string;
  isAdmin: boolean;
  isAPIKeySession: boolean;
  logout: () => void;
  roleLabel: string;
  user: UserResp | null;
}

export function useShellIdentity(): ShellIdentity {
  const { user, logout, isAPIKeySession: authAPIKeySession } = useAuth();
  const { t } = useTranslation();
  const site = useSiteSettings();

  const isAPIKeySession = authAPIKeySession || user?.role === 'api_key' || !!(user?.api_key_id && user.api_key_id > 0);
  const isAdmin = !isAPIKeySession && (getTokenRole() === 'admin' || user?.role === 'admin');
  const displayName = user?.username || user?.email?.split('@')[0] || site.site_name || 'AirGate';
  const roleLabel = user?.role === 'api_key'
    ? 'API Key'
    : isAdmin ? t('users.role_admin', 'Admin') : t('users.role_user', 'User');
  const balanceValue = typeof user?.balance === 'number' && Number.isFinite(user.balance)
    ? user.balance
    : null;
  const balanceText = balanceValue === null ? '' : `$${balanceValue.toFixed(4)}`;

  return useMemo(() => ({
    balanceText,
    balanceValue,
    displayName,
    isAdmin,
    isAPIKeySession,
    logout,
    roleLabel,
    user,
  }), [
    balanceText,
    balanceValue,
    displayName,
    isAdmin,
    isAPIKeySession,
    logout,
    roleLabel,
    user,
  ]);
}
