import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { UserResp } from '../../shared/types';
import {
  setToken,
  getToken,
  setSessionAPIKey,
  getTokenAPIKeyID,
  getTokenRole,
} from '../../shared/api/client';
import { usersApi } from '../../shared/api/users';
import { resetAdminCache } from '../routeGuards';

interface AuthContextType {
  user: UserResp | null;
  loading: boolean;
  /** 是否为 API Key 登录 */
  isAPIKeySession: boolean;
  login: (token: string, user: UserResp) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  isAPIKeySession: false,
  login: () => {},
  logout: () => {},
});

const USER_BALANCE_EVENT = 'airgate:user-balance-updated';

function normalizeSessionUser(user: UserResp, token = getToken()): UserResp {
  const role = getTokenRole(token);
  const apiKeyID = getTokenAPIKeyID(token);
  const effectiveRole: UserResp['role'] = apiKeyID ? 'api_key' : (role ?? user.role);

  return {
    ...user,
    role: effectiveRole,
    ...(apiKeyID ? { api_key_id: apiKeyID } : {}),
  };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<UserResp | null>(null);
  const [loading, setLoading] = useState(true);
  const authRevisionRef = useRef(0);

  useEffect(() => {
    let cancelled = false;
    const revision = authRevisionRef.current;
    const token = getToken();
    if (token) {
      usersApi.me()
        .then((userData) => {
          const currentToken = getToken();
          if (!cancelled && authRevisionRef.current === revision && currentToken) {
            setUser(normalizeSessionUser(userData, currentToken));
          }
        })
        .catch(() => {
          if (!cancelled && authRevisionRef.current === revision && getToken() === token) {
            resetAdminCache();
            setToken(null);
            setUser(null);
          }
        })
        .finally(() => {
          if (!cancelled && authRevisionRef.current === revision) setLoading(false);
        });
    } else {
      setLoading(false);
    }

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const handleUserBalanceUpdate = (event: Event) => {
      const balance = (event as CustomEvent<{ balance?: unknown }>).detail?.balance;
      if (typeof balance !== 'number' || !Number.isFinite(balance)) return;

      setUser((current) => (
        current ? { ...current, balance } : current
      ));
    };

    window.addEventListener(USER_BALANCE_EVENT, handleUserBalanceUpdate);
    return () => window.removeEventListener(USER_BALANCE_EVENT, handleUserBalanceUpdate);
  }, []);

  const login = useCallback((token: string, userData: UserResp) => {
    authRevisionRef.current += 1;
    const revision = authRevisionRef.current;
    resetAdminCache();
    setToken(token);
    setUser(normalizeSessionUser(userData, token));
    // 登录响应可能不包含全部用户字段（例如 API Key 登录时缺少 quota / expires_at），
    // 异步用 /me 拉一次完整数据补齐，避免首屏额度等信息显示不准。
    usersApi.me()
      .then((freshUser) => {
        const currentToken = getToken();
        if (authRevisionRef.current === revision && currentToken) {
          setUser(normalizeSessionUser(freshUser, currentToken));
        }
      })
      .catch(() => {});
  }, []);

  const logout = useCallback(() => {
    authRevisionRef.current += 1;
    setToken(null);
    setSessionAPIKey(null);
    setUser(null);
    resetAdminCache();
    window.location.href = '/login';
  }, []);

  const isAPIKeySession = user?.role === 'api_key' || !!(user?.api_key_id && user.api_key_id > 0);
  const value = useMemo(
    () => ({ user, loading, isAPIKeySession, login, logout }),
    [isAPIKeySession, loading, login, logout, user],
  );

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
