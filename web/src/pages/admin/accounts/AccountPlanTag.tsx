import styles from './AccountIdentityCell.module.css';

// Pure presentation shared by table rows and filter options; no subscriptions.
export function AccountPlanTag({ label, planKey, title = label, maxCharacters = 4 }: { label: string; planKey: string; title?: string; maxCharacters?: 4 | 8 }) {
  return <span className={styles.plan} data-plan={planKey} title={title} aria-label={label}>
    {Array.from(label).slice(0, maxCharacters).join('')}
  </span>;
}
