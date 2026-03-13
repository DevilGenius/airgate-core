import { useTranslation } from 'react-i18next';
import { Inbox } from 'lucide-react';
import { type ReactNode } from 'react';

interface EmptyStateProps {
  title?: string;
  description?: string;
  icon?: ReactNode;
}

export function EmptyState({
  title,
  description,
  icon,
}: EmptyStateProps) {
  const { t } = useTranslation();
  const displayTitle = title ?? t('common.no_data');
  const displayDescription = description ?? t('common.no_data_desc');

  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <div className="flex items-center justify-center w-12 h-12 rounded-full bg-surface mb-4">
        {icon || <Inbox className="w-5 h-5 text-text-tertiary" />}
      </div>
      <p className="text-sm font-medium text-text-secondary">{displayTitle}</p>
      <p className="text-xs text-text-tertiary mt-1">{displayDescription}</p>
    </div>
  );
}
