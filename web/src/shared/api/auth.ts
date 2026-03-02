import { post } from './client';
import type { LoginReq, LoginResp, RegisterReq, TOTPSetupResp, TOTPVerifyReq, RefreshResp } from '../types';

export const authApi = {
  login: (data: LoginReq) => post<LoginResp>('/api/v1/auth/login', data),
  register: (data: RegisterReq) => post<void>('/api/v1/auth/register', data),
  totpSetup: () => post<TOTPSetupResp>('/api/v1/auth/totp/setup'),
  totpVerify: (data: TOTPVerifyReq) => post<void>('/api/v1/auth/totp/verify', data),
  totpDisable: (data: TOTPVerifyReq) => post<void>('/api/v1/auth/totp/disable', data),
  refresh: () => post<RefreshResp>('/api/v1/auth/refresh'),
};
