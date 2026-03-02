import { get, put } from './client';
import type { SettingResp, UpdateSettingsReq } from '../types';

export const settingsApi = {
  list: () => get<SettingResp[]>('/api/v1/admin/settings'),
  update: (data: UpdateSettingsReq) => put<void>('/api/v1/admin/settings', data),
};
