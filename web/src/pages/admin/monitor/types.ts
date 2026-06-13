import type { ReactNode } from 'react';
import type { MonitorEventResp, MonitorRequestEventResp } from '../../../shared/types';

export type SelectOption = {
  id: string;
  label: string;
};

export type MonitorColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorEventResp) => ReactNode;
};

export type MonitorRequestColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorRequestEventResp) => ReactNode;
};

export type MonitorTableKey = 'events' | 'requests';
export type MonitorTableRow = MonitorEventResp | MonitorRequestEventResp;
export type MonitorTimeRangePreset = 'all' | '5m' | '30m' | '1h' | '6h' | '24h' | 'custom';

export type MonitorTableColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorTableRow) => ReactNode;
};
