import type { ReactNode } from 'react';

type TableRowActionTone = 'danger' | 'default' | 'info' | 'muted' | 'primary' | 'success' | 'warning';

export function TableRowActionButton({
  ariaBusy = false,
  ariaLabel,
  children,
  isDisabled = false,
  onClick,
  title,
  tone = 'default',
}: {
  ariaBusy?: boolean;
  ariaLabel: string;
  children: ReactNode;
  isDisabled?: boolean;
  onClick: () => void;
  title?: string;
  tone?: TableRowActionTone;
}) {
  return (
    <button
      type="button"
      aria-busy={ariaBusy || undefined}
      aria-label={ariaLabel}
      className="ag-table-row-native-action"
      data-tone={tone}
      disabled={isDisabled}
      title={title ?? ariaLabel}
      onClick={(event) => {
        event.stopPropagation();
        if (isDisabled) return;
        onClick();
      }}
    >
      <span className="sr-only">{ariaLabel}</span>
      <span aria-hidden="true" className="ag-table-row-native-action__label">{children}</span>
    </button>
  );
}
