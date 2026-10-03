import { describe, expect, it } from 'vitest';
import { bulkAccountTestStatus } from './BulkAccountTestModal';

describe('bulk account test classification', () => {
  it('counts cognition failures separately from ordinary failures', () => {
    expect(bulkAccountTestStatus({ success: false, cognitionDegraded: true, error: '未命中正则' })).toBe('cognition');
    expect(bulkAccountTestStatus({ success: false, error: '连接失败' })).toBe('error');
  });
  it('preserves success and rate-limit categories', () => {
    expect(bulkAccountTestStatus({ success: true, cognitionDegraded: false })).toBe('success');
    expect(bulkAccountTestStatus({ success: true })).toBe('success');
    expect(bulkAccountTestStatus({ success: false, cognitionDegraded: true, error: 'HTTP 429' })).toBe('warning');
  });
});
