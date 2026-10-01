import { memo, useMemo, useSyncExternalStore } from 'react';
import { KeyRound, Link, Server } from 'lucide-react';
import { getPluginPlatformIcon, getPlatformIconVersion, onPlatformIconChange } from '../../../app/plugin-frontend-registry';
import styles from './AccountFilterIcons.module.css';
import { compareAccountFilterPlatforms } from './accountFilterOrder';
import { NO_ACCOUNT_FILTER } from './accountFilterConstants';

type Option = { key: string; label: string };
const AUTH_OPTIONS = [{ key: 'oauth', label: 'OAuth', Icon: Link }, { key: 'apikey', label: 'API Key', Icon: KeyRound }] as const;

export const AccountFilterIcons = memo(function AccountFilterIcons({
  platforms, selectedPlatforms, selectedTypes, platformLabel, typeLabel, onPlatformsChange, onTypesChange,
}: {
  platforms: Option[];
  selectedPlatforms: readonly string[];
  selectedTypes: readonly string[];
  platformLabel: string;
  typeLabel: string;
  onPlatformsChange: (keys: string[]) => void;
  onTypesChange: (keys: string[]) => void;
}) {
  useSyncExternalStore(onPlatformIconChange, getPlatformIconVersion, getPlatformIconVersion);
  const orderedPlatforms = useMemo(() => [...platforms].sort((a, b) => compareAccountFilterPlatforms(a.key, b.key)), [platforms]);
  // Empty stored selections mean all; the explicit marker keeps every icon off.
  const platformKeys = orderedPlatforms.map(({ key }) => key);
  const authKeys = AUTH_OPTIONS.map(({ key }) => key);
  const activePlatforms = selectedPlatforms.length ? selectedPlatforms : platformKeys;
  const activeTypes = selectedTypes.length ? selectedTypes : authKeys;
  const toggle = (selected: readonly string[], key: string, all: readonly string[]) => {
    const current = selected.filter((value) => value !== NO_ACCOUNT_FILTER);
    const next = current.includes(key) ? current.filter((value) => value !== key) : [...current, key];
    if (next.length === 0) return [NO_ACCOUNT_FILTER];
    return all.every((value) => next.includes(value)) ? [] : next;
  };
  return (
    <div className={styles.filters}>
      <div className={styles.group} role="group" aria-label={platformLabel}>
        {orderedPlatforms.map(({ key, label }) => {
          const Icon = getPluginPlatformIcon(key) ?? Server;
          return <button key={key} type="button" className={styles.toggle} title={label} aria-label={label}
            aria-pressed={activePlatforms.includes(key)} onClick={() => onPlatformsChange(toggle(activePlatforms, key, platformKeys))}>
            <Icon className={styles.icon} />
          </button>;
        })}
      </div>
      <div className={styles.group} role="group" aria-label={typeLabel}>
        {AUTH_OPTIONS.map(({ key, label, Icon }) => (
          <button key={key} type="button" className={styles.toggle} title={label} aria-label={label}
            aria-pressed={activeTypes.includes(key)} onClick={() => onTypesChange(toggle(activeTypes, key, authKeys))}>
            <Icon className={styles.icon} aria-hidden="true" />
          </button>
        ))}
      </div>
    </div>
  );
});
