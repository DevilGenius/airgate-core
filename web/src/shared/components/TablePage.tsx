import { memo, type ReactNode } from 'react';
import { EmptyState } from '@heroui/react';
import { Inbox } from 'lucide-react';
import { PageFooterPortal } from './PageFooter';

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

export function TablePage({
  actions,
  children,
  className,
  footer,
  isFetching = false,
  mobile,
  toolbar,
}: {
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  footer?: ReactNode;
  isFetching?: boolean;
  mobile?: ReactNode;
  toolbar?: ReactNode;
}) {
  return (
    <>
      <div className={cx('ag-table-page', className)}>
        {(toolbar || actions) ? (
          <div className="ag-page-toolbar">
            {toolbar ? <div className="ag-page-toolbar-filters">{toolbar}</div> : <div />}
            {actions ? <div className="ag-page-toolbar-actions">{actions}</div> : null}
          </div>
        ) : null}
        <div className={cx('ag-table-page-content', isFetching && 'ag-table-page-content--fetching')}>
          <div className={mobile ? 'ag-table-page-desktop' : undefined}>{children}</div>
          {mobile ? <div className="ag-table-page-mobile">{mobile}</div> : null}
        </div>
      </div>
      {footer ? (
        <PageFooterPortal>
          <div className="ag-table-page-footer">{footer}</div>
        </PageFooterPortal>
      ) : null}
    </>
  );
}

export const TableEmptyState = memo(function TableEmptyState({
  description,
  title,
}: {
  description?: string;
  title: string;
}) {
  return (
    <EmptyState className="flex min-h-[220px] w-full flex-col items-center justify-center gap-3 text-center">
      <div className="flex h-11 w-11 items-center justify-center rounded-[var(--field-radius)] bg-default text-muted shadow-sm">
        <Inbox className="h-5 w-5" />
      </div>
      <div className="space-y-1">
        <div className="text-sm font-medium text-text">{title}</div>
        {description ? (
          <div className="text-xs text-text-tertiary">{description}</div>
        ) : null}
      </div>
    </EmptyState>
  );
});
