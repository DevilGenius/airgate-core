import { type ReactNode } from 'react';

/* ==================== Card ==================== */

interface CardProps {
  children: ReactNode;
  className?: string;
  title?: string;
  extra?: ReactNode;
  noPadding?: boolean;
}

export function Card({ children, className = '', title, extra, noPadding }: CardProps) {
  return (
    <div
      className={`rounded-[var(--ag-radius-lg)] border border-[var(--ag-glass-border)] bg-[var(--ag-bg-elevated)] backdrop-blur-sm shadow-[var(--ag-shadow-sm)] ${className}`}
    >
      {title && (
        <div className="flex items-center justify-between px-5 py-4 border-b border-[var(--ag-border)]">
          <h3 className="text-sm font-semibold text-[var(--ag-text)]">{title}</h3>
          {extra}
        </div>
      )}
      <div className={noPadding ? '' : 'p-5'}>{children}</div>
    </div>
  );
}

/* ==================== StatCard ==================== */

interface StatCardProps {
  title: string;
  value: string | number;
  icon?: ReactNode;
  change?: string;
  changeType?: 'up' | 'down';
  accentColor?: string;
}

export function StatCard({ title, value, icon, change, changeType, accentColor = 'var(--ag-primary)' }: StatCardProps) {
  return (
    <div className="group relative overflow-hidden rounded-[var(--ag-radius-lg)] border border-[var(--ag-glass-border)] bg-[var(--ag-bg-elevated)] p-5 transition-all duration-300 hover:border-[var(--ag-border-focus)] hover:shadow-[var(--ag-shadow-glow)]">
      {/* 顶部发光条 */}
      <div
        className="absolute top-0 left-0 right-0 h-[2px] opacity-60 group-hover:opacity-100 transition-opacity"
        style={{ background: `linear-gradient(90deg, transparent, ${accentColor}, transparent)` }}
      />

      <div className="flex items-start justify-between">
        <div className="space-y-2">
          <p className="text-xs font-medium text-[var(--ag-text-tertiary)] uppercase tracking-wider">
            {title}
          </p>
          <p className="text-2xl font-bold tracking-tight" style={{ fontFamily: 'var(--ag-font-mono)' }}>
            {value}
          </p>
          {change && (
            <p
              className={`text-xs font-medium ${changeType === 'up' ? 'text-[var(--ag-success)]' : 'text-[var(--ag-danger)]'}`}
            >
              {changeType === 'up' ? '↑' : '↓'} {change}
            </p>
          )}
        </div>
        {icon && (
          <div
            className="flex items-center justify-center w-10 h-10 rounded-[var(--ag-radius-md)] transition-colors"
            style={{ background: `color-mix(in srgb, ${accentColor} 12%, transparent)`, color: accentColor }}
          >
            {icon}
          </div>
        )}
      </div>
    </div>
  );
}
