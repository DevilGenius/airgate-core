import { memo, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import type { TableRowMoreMenuItem } from './TableRowMoreMenu';

export type TableRowContextMenuPosition = {
  bottom?: number;
  left: number;
  top?: number;
};

export function getTableRowContextMenuPosition(
  clientX: number,
  clientY: number,
  itemCount: number,
): TableRowContextMenuPosition {
  const edge = 8;
  const gap = 6;
  const estimatedMenuWidth = 128;
  const estimatedMenuHeight = 8 + itemCount * 34;
  const maxLeft = Math.max(edge, window.innerWidth - estimatedMenuWidth - edge);
  const left = Math.min(Math.max(edge, clientX - estimatedMenuWidth / 2), maxLeft);
  const top = clientY + gap;

  if (top + estimatedMenuHeight > window.innerHeight - edge) {
    return {
      bottom: Math.max(edge, window.innerHeight - clientY + gap),
      left,
    };
  }

  return { left, top };
}

export const TableRowContextMenu = memo(function TableRowContextMenu({
  ariaLabel,
  items,
  onClose,
  position,
}: {
  ariaLabel: string;
  items: TableRowMoreMenuItem[];
  onClose: () => void;
  position: TableRowContextMenuPosition;
}) {
  const menuRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (typeof document === 'undefined') return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node) || menuRef.current?.contains(target)) return;
      onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };

    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    window.addEventListener('resize', onClose);
    window.addEventListener('scroll', onClose, true);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('resize', onClose);
      window.removeEventListener('scroll', onClose, true);
    };
  }, [onClose]);

  if (items.length === 0 || typeof document === 'undefined') return null;

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      aria-label={ariaLabel}
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
            onClose();
            item.onSelect();
          }}
        >
          <span>{item.label}</span>
        </button>
      ))}
    </div>,
    document.body,
  );
});
