import { useMemo } from 'react';
import { Button } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { CheckCircle2 } from 'lucide-react';
import { MONITOR_COLUMN_WIDTHS, SEVERITY_CLASSES, STATUS_CLASSES } from './constants';
import {
  DetailCell,
  RequestEndpointPrimary,
  StackCell,
  StatusPill,
  TimeCell,
  monitorDetailEntries,
  monitorLocatorContext,
  monitorRequestSubject,
  monitorRequestSubjectContext,
  monitorSubject,
  monitorSubjectContext,
  requestDetailEntries,
  requestEndpointContext,
  requestEndpointLabel,
  requestErrorCodeLabel,
  requestStatusLabel,
  requestStatusToneClass,
} from './MonitorTableCells';
import type { MonitorColumnConfig, MonitorRequestColumnConfig } from './types';

function statusLabel(t: TFunction, value: string): string {
  return t(`monitor.status_${value}`, value);
}

function severityLabel(t: TFunction, value: string): string {
  return t(`monitor.severity_${value}`, value);
}

function requestSeverityLabel(t: TFunction, value: string): string {
  if (value === 'warning') return t('monitor.request_severity_warning', '警告');
  if (value === 'info') return t('monitor.request_severity_info', '信息');
  return severityLabel(t, value);
}

function typeLabel(t: TFunction, value: string): string {
  return t(`monitor.type_${value}`, value);
}

function isManuallyRecoverableEvent(row: { recovery_mode: string }): boolean {
  return row.recovery_mode === 'manual' || row.recovery_mode === 'success';
}

export function useMonitorColumns({
  onResolve,
  resolvePending,
}: {
  onResolve: (id: number) => void;
  resolvePending: boolean;
}): MonitorColumnConfig[] {
  const { t } = useTranslation();

  return useMemo<MonitorColumnConfig[]>(() => [
    {
      key: 'updated_at',
      title: t('monitor.updated_at'),
      width: MONITOR_COLUMN_WIDTHS.time,
      render: (row) => <TimeCell value={row.updated_at} />,
    },
    {
      key: 'severity',
      title: t('monitor.severity'),
      width: MONITOR_COLUMN_WIDTHS.severity,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col items-center justify-center gap-1">
          <StatusPill className={SEVERITY_CLASSES[row.severity] ?? SEVERITY_CLASSES.warning} label={severityLabel(t, row.severity)} />
          <span className="max-w-full truncate text-[11px] leading-none text-text-tertiary" title={typeLabel(t, row.type)}>
            {typeLabel(t, row.type)}
          </span>
        </div>
      ),
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: MONITOR_COLUMN_WIDTHS.event,
      render: (row) => (
        <StackCell
          primary={row.title || typeLabel(t, row.type)}
          primaryClass="font-medium text-text"
          primaryTitle={row.title || undefined}
          secondary={row.message || undefined}
          secondaryTitle={row.message || undefined}
        />
      ),
    },
    {
      key: 'locator',
      title: t('monitor.source'),
      width: MONITOR_COLUMN_WIDTHS.source,
      hideOnMobile: true,
      render: (row) => (
        <StackCell
          mono
          primary={monitorLocatorContext(row) || '-'}
          primaryClass="text-text-secondary"
          primaryTitle={monitorLocatorContext(row) || undefined}
          secondary={row.error_code || undefined}
          secondaryTitle={row.error_code || undefined}
        />
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: MONITOR_COLUMN_WIDTHS.subject,
      render: (row) => (
        <StackCell
          primary={monitorSubject(row)}
          primaryClass="font-medium text-text"
          primaryTitle={monitorSubject(row)}
          secondary={monitorSubjectContext(row) || undefined}
          secondaryTitle={monitorSubjectContext(row) || undefined}
        />
      ),
    },
    {
      key: 'detail',
      title: t('monitor.detail'),
      width: MONITOR_COLUMN_WIDTHS.detail,
      hideOnMobile: true,
      render: (row) => <DetailCell entries={monitorDetailEntries(row)} />,
    },
    {
      key: 'status',
      title: t('monitor.status'),
      width: MONITOR_COLUMN_WIDTHS.status,
      render: (row) => (
        <StatusPill className={STATUS_CLASSES[row.status] ?? STATUS_CLASSES.active} label={statusLabel(t, row.status)} />
      ),
    },
    {
      key: 'actions',
      title: t('common.actions'),
      width: MONITOR_COLUMN_WIDTHS.actions,
      render: (row) => {
        if (!isManuallyRecoverableEvent(row)) {
          return <span className="text-[13px] leading-none text-text-tertiary">-</span>;
        }
        const disabled = row.status !== 'active';
        return (
          <div className="flex h-full w-full items-center justify-center gap-1">
            <Button
              isIconOnly
              aria-label={t('monitor.resolve')}
              className="h-7 w-7 min-w-7"
              isDisabled={disabled || resolvePending}
              size="sm"
              variant="ghost"
              onPress={() => onResolve(row.id)}
            >
              <CheckCircle2 className="h-4 w-4" />
            </Button>
          </div>
        );
      },
    },
  ], [onResolve, resolvePending, t]);
}

export function useMonitorRequestColumns(): MonitorRequestColumnConfig[] {
  const { t } = useTranslation();

  return useMemo<MonitorRequestColumnConfig[]>(() => [
    {
      key: 'created_at',
      title: t('monitor.time'),
      width: MONITOR_COLUMN_WIDTHS.time,
      render: (row) => <TimeCell value={row.created_at} />,
    },
    {
      key: 'severity',
      title: t('monitor.severity'),
      width: MONITOR_COLUMN_WIDTHS.severity,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col items-center justify-center gap-1">
          <StatusPill className={SEVERITY_CLASSES[row.severity] ?? SEVERITY_CLASSES.info} label={requestSeverityLabel(t, row.severity)} />
          <span className="max-w-full truncate text-[11px] leading-none text-text-tertiary" title={typeLabel(t, row.type)}>
            {typeLabel(t, row.type)}
          </span>
        </div>
      ),
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: MONITOR_COLUMN_WIDTHS.event,
      render: (row) => (
        <StackCell
          primary={row.title || typeLabel(t, row.type)}
          primaryClass="font-medium text-text"
          primaryTitle={row.title || undefined}
          secondary={row.message || typeLabel(t, row.type)}
          secondaryTitle={row.message || undefined}
        />
      ),
    },
    {
      key: 'locator',
      title: t('monitor.endpoint'),
      width: MONITOR_COLUMN_WIDTHS.source,
      hideOnMobile: true,
      render: (row) => (
        <StackCell
          mono
          primary={<RequestEndpointPrimary event={row} />}
          primaryClass="text-text-secondary"
          primaryTitle={requestEndpointLabel(row)}
          secondary={requestEndpointContext(row) || undefined}
          secondaryTitle={requestEndpointContext(row) || undefined}
        />
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: MONITOR_COLUMN_WIDTHS.subject,
      render: (row) => (
        <StackCell
          primary={monitorRequestSubject(row)}
          primaryClass="font-medium text-text"
          primaryTitle={monitorRequestSubject(row)}
          secondary={monitorRequestSubjectContext(row) || undefined}
          secondaryTitle={monitorRequestSubjectContext(row) || undefined}
        />
      ),
    },
    {
      key: 'detail',
      title: t('monitor.detail'),
      width: MONITOR_COLUMN_WIDTHS.detail,
      hideOnMobile: true,
      render: (row) => <DetailCell entries={requestDetailEntries(row)} />,
    },
    {
      key: 'status_error_code',
      title: `${t('monitor.http_status')} / ${t('monitor.error_code')}`,
      width: MONITOR_COLUMN_WIDTHS.statusActions,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col items-center justify-center gap-1">
          <span className={`max-w-full truncate font-mono text-[13px] font-medium leading-none ${requestStatusToneClass(row)}`} title={requestStatusLabel(row)}>
            {requestStatusLabel(row)}
          </span>
          <span className="max-w-full truncate font-mono text-[11px] leading-none text-text-tertiary" title={requestErrorCodeLabel(row)}>
            {requestErrorCodeLabel(row)}
          </span>
        </div>
      ),
    },
  ], [t]);
}
