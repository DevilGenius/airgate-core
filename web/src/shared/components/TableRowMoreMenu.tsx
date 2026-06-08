import { memo, useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

type TableRowMenuPosition = {
  bottom?: number;
  right: number;
  top?: number;
};

export type TableRowMoreMenuItem = {
  key: string;
  label: ReactNode;
  onSelect: () => void;
  ariaLabel?: string;
  isDisabled?: boolean;
  tone?: 'danger' | 'default';
};

interface TableRowMoreMenuProps {
  ariaLabel: string;
  items: TableRowMoreMenuItem[];
  menuLabel: string;
}

function getTableRowMenuPosition(trigger: HTMLElement, itemCount: number): TableRowMenuPosition {
  const rect = trigger.getBoundingClientRect();
  const gap = 6;
  const edge = 8;
  const estimatedMenuHeight = 8 + itemCount * 34;
  const right = Math.max(edge, window.innerWidth - rect.right);
  const top = rect.bottom + gap;

  if (top + estimatedMenuHeight > window.innerHeight - edge) {
    return {
      bottom: Math.max(edge, window.innerHeight - rect.top + gap),
      right,
    };
  }

  return {
    right,
    top,
  };
}

export const TableRowMoreMenu = memo(function TableRowMoreMenu({
  ariaLabel,
  items,
  menuLabel,
}: TableRowMoreMenuProps) {
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [position, setPosition] = useState<TableRowMenuPosition | null>(null);
  const isOpen = position !== null;

  const close = useCallback(() => {
    setPosition(null);
  }, []);

  const toggleMenu = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (items.length === 0) return;
    if (isOpen) {
      close();
      return;
    }
    setPosition(getTableRowMenuPosition(event.currentTarget, items.length));
  }, [close, isOpen, items.length]);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };

    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
  }, [close, isOpen]);

  useEffect(() => {
    if (isOpen && items.length === 0) close();
  }, [close, isOpen, items.length]);

  if (items.length === 0) return null;

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={isOpen}
        aria-haspopup="menu"
        aria-label={ariaLabel}
        className="ag-table-row-native-action ag-table-row-more-trigger"
        title={ariaLabel}
        onClick={toggleMenu}
      >
        <span className="sr-only">{ariaLabel}</span>
        <span aria-hidden="true" className="ag-table-row-more-dots" />
      </button>
      {isOpen && position && typeof document !== 'undefined' ? createPortal(
        <div
          ref={menuRef}
          role="menu"
          aria-label={menuLabel}
          className="ag-table-row-menu"
          style={position}
          onClick={(event) => event.stopPropagation()}
        >
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              aria-disabled={item.isDisabled || undefined}
              aria-label={item.ariaLabel}
              className="ag-table-row-menu-item"
              data-tone={item.tone}
              disabled={item.isDisabled}
              role="menuitem"
              onClick={(event) => {
                event.stopPropagation();
                if (item.isDisabled) return;
                close();
                item.onSelect();
              }}
            >
              <span>{item.label}</span>
            </button>
          ))}
        </div>,
        document.body,
      ) : null}
    </>
  );
});
