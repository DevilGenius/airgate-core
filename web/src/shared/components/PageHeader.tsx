import { type ReactNode } from 'react';

interface PageHeaderProps {
  title?: string;
  description?: string;
  actions?: ReactNode;
}

export function PageHeader({ actions }: PageHeaderProps) {
  if (!actions) return null;
  return (
    <div className="flex items-center justify-end mb-6">
      <div className="flex items-center gap-3">{actions}</div>
    </div>
  );
}
