import type { ReactNode } from 'react';

export type NativeSoftChipTone = 'accent' | 'default' | 'success';

export function NativeSoftChip({
  children,
  className,
  title,
  tone,
}: {
  children: ReactNode;
  className?: string;
  title?: string;
  tone: NativeSoftChipTone;
}) {
  return (
    <span className={`ag-native-soft-chip ${className ?? ''}`} data-tone={tone} title={title}>
      {children}
    </span>
  );
}
