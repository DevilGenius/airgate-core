import type { ReactNode } from 'react';
import styles from './ContentPage.module.css';

/** Readable settings/forms use the same gutter as table and overview pages. */
export function ContentPage({ children }: { children: ReactNode }) {
  return <div className={styles.page}>{children}</div>;
}
