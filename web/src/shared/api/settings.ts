import { get, put, post } from './client';
import type { SettingResp, UpdateSettingsReq, TestSMTPReq, TestNotificationReq } from '../types';

export interface CoreVersionInfo {
  version: string;
  go_version: string;
  platform: string;
}

export const settingsApi = {
  list: () => get<SettingResp[]>('/api/v1/admin/settings'),
  update: (data: UpdateSettingsReq) => put<void>('/api/v1/admin/settings', data),
  testSMTP: (data: TestSMTPReq) => post<void>('/api/v1/admin/settings/test-smtp', data),
  testNotification: (data: TestNotificationReq) => post<void>('/api/v1/admin/settings/test-notification', data),
  getPublic: () => get<Record<string, string>>('/api/v1/settings/public'),
  getCoreVersion: () => get<CoreVersionInfo>('/api/v1/admin/version'),
};
