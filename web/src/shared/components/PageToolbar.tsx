import { memo, type ReactNode } from 'react';
import styles from './PageToolbar.module.css';

export function PageToolbarFrame({ children, className, flow = 'page' }: {
  children: ReactNode;
  className?: string;
  flow?: 'page' | 'stack';
}) {
  return (
    <div data-flow={flow} className={['ag-page-toolbar', styles.toolbar, className].filter(Boolean).join(' ')}>
      {children}
    </div>
  );
}

/** A layout boundary only: filters and actions retain their own state. */
export const PageToolbar = memo(function PageToolbar({
  children,
  actions,
  className,
}: {
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <PageToolbarFrame className={className}>
      {children ? <div className="ag-page-toolbar-filters">{children}</div> : <div />}
      {actions ? <div className="ag-page-toolbar-actions">{actions}</div> : null}
    </PageToolbarFrame>
  );
});
