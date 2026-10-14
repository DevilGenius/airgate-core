import { describe, expect, it } from 'vitest';
import { accountTestResultStatus } from './accountTestRunner';

describe('account test result classification', () => {
  it('counts cognition failures separately from ordinary failures', () => {
    expect(accountTestResultStatus({ success: false, cognitionDegraded: true, error: '降智检测未命中正常回复正则，已标记降智' })).toBe('cognition');
    expect(accountTestResultStatus({ success: false, error: '连接失败' })).toBe('error');
  });
  it.each([
    'HTTP 401: invalid_request_error: 认证失败，access_token 已过期或账号已被停用: Encountered invalidated oauth token for user, failing request (HTTP 401)',
    'HTTP 403: Forbidden',
    'HTTP 500: Internal Server Error',
    '连接失败',
  ])('keeps upstream failures red despite the degradation flag: %s', (error) => {
    expect(accountTestResultStatus({ success: false, cognitionDegraded: true, error })).toBe('error');
  });
  it('preserves success and rate-limit categories', () => {
    expect(accountTestResultStatus({ success: true, cognitionDegraded: false })).toBe('success');
    expect(accountTestResultStatus({ success: true })).toBe('success');
    expect(accountTestResultStatus({ success: false, cognitionDegraded: true, error: 'HTTP 429' })).toBe('warning');
  });
  it.each([
    '上游账号当前被限流，请稍后重试',
    '上游账号当前被限流，请在 3m0s 后重试',
    'rate_limit_exceeded',
    'Too many requests',
  ])('prioritizes rate limiting over cognition degradation: %s', (error) => {
    expect(accountTestResultStatus({ success: false, cognitionDegraded: true, error })).toBe('warning');
  });
});
