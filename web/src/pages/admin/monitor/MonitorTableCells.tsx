import type { ReactNode } from 'react';
import type { MonitorEventResp, MonitorRequestEventResp } from '../../../shared/types';

type DetailEntry = {
  key: string;
  value: string;
};

const MONITOR_TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  hour12: false,
  minute: '2-digit',
  second: '2-digit',
});
const MONITOR_DATE_FORMATTER = new Intl.DateTimeFormat('zh-CN');

function monitorTimeLabels(value?: string) {
  if (!value) {
    return { dateLabel: '', fullLabel: '-', timeLabel: '-' };
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return { dateLabel: '', fullLabel: value, timeLabel: value };
  }
  const timeLabel = MONITOR_TIME_FORMATTER.format(date);
  const dateLabel = MONITOR_DATE_FORMATTER.format(date);
  return { dateLabel, fullLabel: `${dateLabel} ${timeLabel}`, timeLabel };
}

function detailString(detail: Record<string, unknown> | undefined, key: string): string {
  const value = detail?.[key];
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  return '';
}

function detailNumber(detail: Record<string, unknown> | undefined, key: string): number | undefined {
  const value = detail?.[key];
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

function namedIDLabel(name?: string, id?: number | string): string {
  const trimmedName = name?.trim() ?? '';
  const idText = id === undefined || id === null || id === '' ? '' : String(id);
  if (trimmedName && idText) return `${trimmedName} #${idText}`;
  if (trimmedName) return trimmedName;
  if (idText) return `#${idText}`;
  return '';
}

function monitorGroupLabel(event: MonitorEventResp | MonitorRequestEventResp): string {
  const detail = event.detail;
  const groupName = detailString(detail, 'group_name') || detailString(detail, 'group_name_snapshot');
  const groupID = 'group_id' in event && event.group_id !== undefined
    ? event.group_id
    : detailNumber(detail, 'group_id') ?? ('subject_type' in event && event.subject_type === 'scheduler' ? event.subject_id : undefined);
  return namedIDLabel(groupName, groupID);
}

function monitorAccountLabel(event: MonitorEventResp | MonitorRequestEventResp): string {
  return namedIDLabel(event.account_name_snapshot, event.account_id);
}

export function monitorSubject(event: MonitorEventResp): string {
  return monitorAccountLabel(event)
    || monitorGroupLabel(event)
    || namedIDLabel(event.plugin_id, event.subject_id)
    || event.subject_id
    || '-';
}

export function monitorSubjectContext(event: MonitorEventResp): string {
  return [event.subject_type, event.platform].filter(Boolean).join(' › ');
}

export function monitorLocatorContext(event: MonitorEventResp): string {
  return [event.source, event.plugin_id].filter(Boolean).join(' › ');
}

export function monitorRequestSubject(event: MonitorRequestEventResp): string {
  const userLabel = event.user_email_snapshot || namedIDLabel(undefined, event.user_id);
  const apiKeyLabel = namedIDLabel(event.api_key_name_snapshot, event.api_key_id);
  return [apiKeyLabel, userLabel].filter(Boolean).join(' › ')
    || monitorAccountLabel(event)
    || monitorGroupLabel(event)
    || event.plugin_id
    || event.request_id
    || '-';
}

export function monitorRequestSubjectContext(event: MonitorRequestEventResp): string {
  return [
    monitorAccountLabel(event),
    monitorGroupLabel(event),
    event.platform,
  ].filter(Boolean).join(' › ');
}

export function requestEndpointLabel(event: MonitorRequestEventResp): string {
  return [event.method, event.endpoint].filter(Boolean).join(' ') || event.source || '-';
}

export function RequestEndpointPrimary({ event }: { event: MonitorRequestEventResp }) {
  if (!event.method) return <>{event.endpoint || event.source || '-'}</>;
  return (
    <>
      <span className="font-semibold text-emerald-600 dark:text-emerald-400">{event.method}</span>
      {event.endpoint ? <span className="text-text-secondary"> {event.endpoint}</span> : null}
    </>
  );
}

export function requestEndpointContext(event: MonitorRequestEventResp): string {
  return [event.model, event.plugin_id].filter(Boolean).join(' › ');
}

export function requestStatusLabel(event: MonitorRequestEventResp): string {
  if (event.http_status && event.upstream_status && event.upstream_status !== event.http_status) {
    return `${event.http_status} / ${event.upstream_status}`;
  }
  return String(event.http_status || event.upstream_status || '-');
}

export function requestErrorCodeLabel(event: MonitorRequestEventResp): string {
  return event.error_code || '-';
}

export function requestStatusToneClass(event: MonitorRequestEventResp): string {
  const status = Math.max(event.http_status ?? 0, event.upstream_status ?? 0);
  if (status >= 500) return 'text-danger';
  if (status >= 400) return 'text-amber-600 dark:text-amber-400';
  return 'text-text-secondary';
}

function detailValue(detail: Record<string, unknown> | undefined, key: string): string {
  const value = detail?.[key];
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  return '';
}

function durationMsLabel(value?: number | string): string {
  const duration = typeof value === 'string' ? Number(value) : value;
  if (!duration || !Number.isFinite(duration) || duration <= 0) return '';
  if (duration >= 10_000) return `${(duration / 1000).toFixed(1)}s`;
  if (duration >= 1000) return `${(duration / 1000).toFixed(2)}s`;
  return `${duration}ms`;
}

function appendDetail(entries: DetailEntry[], key: string, value?: string | number | boolean) {
  const normalized = typeof value === 'string'
    ? value.trim()
    : typeof value === 'number' && Number.isFinite(value)
      ? String(value)
      : typeof value === 'boolean'
        ? value ? 'true' : 'false'
        : '';
  if (!normalized) return;
  if (entries.some((item) => item.key === key && item.value === normalized)) return;
  entries.push({ key, value: normalized });
}

function detailText(entries: DetailEntry[]): string {
  return entries.map((entry) => `${entry.key}=${entry.value}`).join(' › ');
}

function detailJsonText(entries: DetailEntry[]): string {
  const detail: Record<string, string | string[]> = {};
  for (const entry of entries) {
    const existing = detail[entry.key];
    if (existing === undefined) {
      detail[entry.key] = entry.value;
    } else if (Array.isArray(existing)) {
      existing.push(entry.value);
    } else {
      detail[entry.key] = [existing, entry.value];
    }
  }
  return JSON.stringify(detail, null, 2);
}

export function monitorDetailEntries(event: MonitorEventResp): DetailEntry[] {
  const detail = event.detail;
  const entries: DetailEntry[] = [];
  appendDetail(entries, 'retry_count', detailValue(detail, 'retry_count'));
  appendDetail(entries, 'attempts', detailValue(detail, 'attempts'));
  appendDetail(entries, 'model', detailValue(detail, 'model'));
  appendDetail(entries, 'client_model', detailValue(detail, 'client_model'));
  appendDetail(entries, 'http_status', detailValue(detail, 'http_status'));
  appendDetail(entries, 'request_id', detailValue(detail, 'request_id'));
  appendDetail(entries, 'request_path', detailValue(detail, 'request_path'));
  appendDetail(entries, 'stage', detailValue(detail, 'stage'));
  appendDetail(entries, 'duration_ms', durationMsLabel(detailValue(detail, 'duration_ms')));
  return entries;
}

export function requestDetailEntries(event: MonitorRequestEventResp): DetailEntry[] {
  const detail = event.detail;
  const entries: DetailEntry[] = [];
  appendDetail(entries, 'retry_count', detailValue(detail, 'retry_count'));
  appendDetail(entries, 'attempts', detailValue(detail, 'attempts'));
  appendDetail(entries, 'next_attempt', detailValue(detail, 'next_attempt'));
  appendDetail(entries, 'duration_ms', durationMsLabel(event.duration_ms || detailValue(detail, 'duration_ms')));
  appendDetail(entries, 'request_id', event.request_id);
  appendDetail(entries, 'fingerprint', event.fingerprint);
  appendDetail(entries, 'upstream_status', event.upstream_status && event.upstream_status !== event.http_status ? event.upstream_status : undefined);
  appendDetail(entries, 'stage', detailValue(detail, 'stage'));
  appendDetail(entries, 'outcome_kind', detailValue(detail, 'outcome_kind'));
  appendDetail(entries, 'reason', detailValue(detail, 'reason'));
  return entries;
}

export function TimeCell({ value }: { value?: string }) {
  const { dateLabel, fullLabel, timeLabel } = monitorTimeLabels(value);
  return (
    <div className="flex min-w-0 flex-col justify-center gap-1 text-left" title={fullLabel}>
      <span className="truncate font-mono text-[13px] font-medium leading-none text-text">{timeLabel}</span>
      {dateLabel ? (
        <span className="truncate font-mono text-[11px] leading-none text-text-tertiary">{dateLabel}</span>
      ) : null}
    </div>
  );
}

export function StackCell({
  mono,
  primary,
  primaryClass = 'text-text',
  primaryTitle,
  secondary,
  secondaryTitle,
}: {
  mono?: boolean;
  primary: ReactNode;
  primaryClass?: string;
  primaryTitle?: string;
  secondary?: ReactNode;
  secondaryTitle?: string;
}) {
  return (
    <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
      <span className={`truncate text-[13px] leading-none ${mono ? 'font-mono ' : ''}${primaryClass}`} title={primaryTitle}>
        {primary}
      </span>
      {secondary ? (
        <span className="truncate text-[11px] leading-none text-text-tertiary" title={secondaryTitle}>
          {secondary}
        </span>
      ) : null}
    </div>
  );
}

export function StatusPill({ className, label }: { className?: string; label: string }) {
  return (
    <span className={`inline-flex h-5 max-w-full items-center justify-center truncate rounded-[var(--radius)] px-2 text-xs font-medium leading-none ring-1 ${className ?? ''}`}>
      {label}
    </span>
  );
}

export function DetailCell({ entries }: { entries: DetailEntry[] }) {
  if (entries.length === 0) {
    return <span className="block w-full truncate text-left text-[13px] leading-none text-text-tertiary">-</span>;
  }
  const title = detailJsonText(entries);
  const primary = detailText(entries.slice(0, 2));
  const secondary = detailText(entries.slice(2));
  return (
    <StackCell
      mono
      primary={primary}
      primaryClass="text-text-secondary"
      primaryTitle={title}
      secondary={secondary || undefined}
      secondaryTitle={secondary ? title : undefined}
    />
  );
}
