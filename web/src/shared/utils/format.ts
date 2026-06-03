import i18n from '../../i18n';

/** 格式化过期时间，未设置则显示"永不过期" */
export function formatExpiry(date?: string, neverLabel?: string): string {
  if (!date) return neverLabel ?? i18n.t('common.never_expire');
  return new Date(date).toLocaleDateString('zh-CN');
}

export function formatDateInputValue(date?: string): string {
  if (!date) return '';
  const parsed = new Date(date);
  if (Number.isNaN(parsed.getTime())) return '';
  const year = parsed.getFullYear();
  const month = String(parsed.getMonth() + 1).padStart(2, '0');
  const day = String(parsed.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

export function dateInputToLocalStartRFC3339(value?: string): string {
  if (!value) return '';
  const [year, month, day] = value.split('-').map(Number);
  if (!year || !month || !day) return '';
  return new Date(year, month - 1, day, 0, 0, 0, 0).toISOString().replace('.000Z', 'Z');
}

/** 格式化日期时间 (yyyy/M/d HH:mm) */
export function formatDateTime(date: string): string {
  return new Date(date).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** 格式化日期 (yyyy/M/d) */
export function formatDate(date: string): string {
  return new Date(date).toLocaleDateString('zh-CN');
}

export function formatAPIKeyHint(value?: string): string {
  const key = value?.trim() ?? '';
  if (!key || !key.startsWith('sk') || key.includes('...') || key.length <= 11) return key;
  return `${key.slice(0, 7)}...${key.slice(-4)}`;
}
