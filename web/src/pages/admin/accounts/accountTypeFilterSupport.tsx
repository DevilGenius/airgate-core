import type { ComponentType, ReactNode } from 'react';
import type { PluginPlatformIconProps } from '@devilgenius/airgate-theme/plugin';
import { normalizeAccountPlan } from '../../../shared/utils/accountPlan';
import { AccountPlatformTypeIcons } from './AccountPlatformTypeIcons';
import { AccountPlanTag } from './AccountPlanTag';
import styles from './accountTypeFilterSupport.module.css';

export type AccountTypeFilterOption = {
  id: string;
  label: string;
  planLabel?: string;
  platform?: string;
  platformLabel?: string;
};

export function renderAccountTypeFilterOption(option: AccountTypeFilterOption, PlatformGlyph: ComponentType<PluginPlatformIconProps>): ReactNode {
  if (!option.planLabel) return option.label;
  return (
    <span className={styles.option} title={option.label} aria-label={option.label}>
      <AccountPlatformTypeIcons PlatformGlyph={PlatformGlyph} platformLabel={option.platformLabel ?? option.platform ?? ''} />
      <span className={styles.tag}>
        <AccountPlanTag label={option.planLabel} planKey={normalizeAccountPlan(option.planLabel)} maxCharacters={8} />
      </span>
    </span>
  );
}
