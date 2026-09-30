import type { ReactNode } from 'react';
import { Card } from './Card';
import styles from './Panel.module.css';

/** Chart/detail panel shared by overview and reporting pages. */
export function Panel({ children, extra, title }: {
  children: ReactNode;
  extra?: ReactNode;
  title?: ReactNode;
}) {
  const hasHeader = Boolean(title || extra);
  return (
    <Card className={`ag-dashboard-panel ${styles.panel}`}>
      {hasHeader ? (
        <div className={styles.header}>
          {title ? <h2 className={styles.title}>{title}</h2> : null}
          {extra ? <div className={styles.actions}>{extra}</div> : null}
        </div>
      ) : null}
      <Card.Content className={styles.content} data-has-header={hasHeader || undefined}>
        {children}
      </Card.Content>
    </Card>
  );
}
