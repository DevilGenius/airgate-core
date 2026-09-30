import { memo, type ComponentType } from 'react';
import type { PluginPlatformIconProps } from '@devilgenius/airgate-theme/plugin';
import { AccountTypeIcon } from './AccountTypeIcon';
import styles from './AccountIdentityCell.module.css';

// 弹窗在顶层订阅图标注册；流式输出更新不会重绘未变化的图标组。
export const AccountPlatformTypeIcons = memo(function AccountPlatformTypeIcons({
  PlatformGlyph, platformLabel, type, typeLabel, size = 'sm',
}: {
  PlatformGlyph: ComponentType<PluginPlatformIconProps>;
  platformLabel: string;
  type?: string;
  typeLabel?: string;
  size?: 'sm' | 'md' | 'lg';
}) {
  return (
    <span className={styles.icons} data-size={size}>
      <span className={styles.icon} role="img" title={platformLabel} aria-label={platformLabel}>
        <PlatformGlyph className="size-3.5" />
      </span>
      {type ? <AccountTypeIcon type={type === 'api_key' ? 'apikey' : type} label={typeLabel} /> : null}
    </span>
  );
});
