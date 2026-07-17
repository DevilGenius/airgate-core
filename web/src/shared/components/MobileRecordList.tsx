import {
  memo,
  useCallback,
  useEffect,
  useRef,
  useState,
  type AnimationEventHandler,
  type KeyboardEventHandler,
  type MouseEventHandler,
  type PointerEventHandler,
  type ReactNode,
} from 'react';
import { TableEmptyState } from './TablePage';
import {
  getTableRowContextMenuPosition,
  TableRowContextMenu,
  type TableRowContextMenuPosition,
} from './TableRowContextMenu';
import type { TableRowMoreMenuItem } from './TableRowMoreMenu';

const LONG_PRESS_DURATION_MS = 550;
const LONG_PRESS_MOVE_TOLERANCE_PX = 10;

export interface MobileRecordLongPressMenu {
  ariaLabel: string;
  items: TableRowMoreMenuItem[];
}

export interface MobileRecordField {
  className?: string;
  label: ReactNode;
  value: ReactNode;
}

export interface MobileRecordItem {
  className?: string;
  onAnimationEnd?: AnimationEventHandler<HTMLElement>;
  id: string | number;
  title: ReactNode;
  description?: ReactNode;
  meta?: ReactNode;
  fields?: MobileRecordField[];
  actions?: ReactNode;
  longPressMenu?: MobileRecordLongPressMenu;
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element
    && target.closest('a, button, input, select, textarea, [role="button"], [role="menuitem"]') !== null;
}

function MobileRecordCard({ item }: { item: MobileRecordItem }) {
  const longPressTimerRef = useRef<number | null>(null);
  const pointerStartRef = useRef<{ pointerId: number; x: number; y: number } | null>(null);
  const [menuPosition, setMenuPosition] = useState<TableRowContextMenuPosition | null>(null);
  const menuEnabled = Boolean(item.longPressMenu?.items.length);

  const clearLongPress = useCallback(() => {
    if (longPressTimerRef.current !== null) {
      window.clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
    pointerStartRef.current = null;
  }, []);

  const closeMenu = useCallback(() => {
    setMenuPosition(null);
  }, []);

  const openMenu = useCallback((clientX: number, clientY: number) => {
    const itemCount = item.longPressMenu?.items.length ?? 0;
    if (itemCount === 0) return;
    setMenuPosition(getTableRowContextMenuPosition(clientX, clientY, itemCount));
  }, [item.longPressMenu?.items.length]);

  useEffect(() => () => clearLongPress(), [clearLongPress]);

  const handlePointerDown: PointerEventHandler<HTMLElement> = (event) => {
    if (!menuEnabled || !event.isPrimary || event.button !== 0 || isInteractiveTarget(event.target)) return;

    clearLongPress();
    pointerStartRef.current = {
      pointerId: event.pointerId,
      x: event.clientX,
      y: event.clientY,
    };
    longPressTimerRef.current = window.setTimeout(() => {
      const pointerStart = pointerStartRef.current;
      longPressTimerRef.current = null;
      pointerStartRef.current = null;
      if (pointerStart) openMenu(pointerStart.x, pointerStart.y);
    }, LONG_PRESS_DURATION_MS);
  };

  const handlePointerMove: PointerEventHandler<HTMLElement> = (event) => {
    const pointerStart = pointerStartRef.current;
    if (!pointerStart || pointerStart.pointerId !== event.pointerId) return;
    if (Math.hypot(event.clientX - pointerStart.x, event.clientY - pointerStart.y) > LONG_PRESS_MOVE_TOLERANCE_PX) {
      clearLongPress();
    }
  };

  const handleContextMenu: MouseEventHandler<HTMLElement> = (event) => {
    if (!menuEnabled) return;
    event.preventDefault();
    clearLongPress();
    if (!isInteractiveTarget(event.target)) openMenu(event.clientX, event.clientY);
  };

  const handleKeyDown: KeyboardEventHandler<HTMLElement> = (event) => {
    if (!menuEnabled || (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10'))) return;
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    openMenu(rect.left + rect.width / 2, rect.top + rect.height / 2);
  };

  return (
    <>
      <article
        aria-haspopup={menuEnabled ? 'menu' : undefined}
        className={['ag-mobile-record-card', item.className].filter(Boolean).join(' ')}
        onAnimationEnd={item.onAnimationEnd}
        onContextMenu={handleContextMenu}
        onKeyDown={handleKeyDown}
        onPointerCancel={clearLongPress}
        onPointerDown={handlePointerDown}
        onPointerLeave={clearLongPress}
        onPointerMove={handlePointerMove}
        onPointerUp={clearLongPress}
      >
        <div className="ag-mobile-record-head">
          <div className="min-w-0">
            <div className="ag-mobile-record-title">{item.title}</div>
            {item.description ? <div className="ag-mobile-record-description">{item.description}</div> : null}
          </div>
          {item.meta ? <div className="ag-mobile-record-meta">{item.meta}</div> : null}
        </div>
        {item.fields?.length ? (
          <dl className="ag-mobile-record-fields">
            {item.fields.map((field, index) => (
              <div className={['ag-mobile-record-field', field.className].filter(Boolean).join(' ')} key={index}>
                <dt>{field.label}</dt>
                <dd>{field.value}</dd>
              </div>
            ))}
          </dl>
        ) : null}
        {item.actions ? <div className="ag-mobile-record-actions">{item.actions}</div> : null}
      </article>
      {menuEnabled && item.longPressMenu && menuPosition ? (
        <TableRowContextMenu
          ariaLabel={item.longPressMenu.ariaLabel}
          items={item.longPressMenu.items}
          position={menuPosition}
          onClose={closeMenu}
        />
      ) : null}
    </>
  );
}

export const MobileRecordList = memo(function MobileRecordList({
  emptyDescription,
  emptyTitle,
  isLoading,
  items,
  loading,
}: {
  emptyDescription?: string;
  emptyTitle: string;
  isLoading?: boolean;
  items: MobileRecordItem[];
  loading?: ReactNode;
}) {
  if (isLoading) {
    return (
      <div className="ag-mobile-record-list">
        {loading ?? Array.from({ length: 4 }, (_, index) => (
          <div className="ag-mobile-record-card ag-mobile-record-card--loading" key={index}>
            <div className="skeleton h-4 w-2/3" />
            <div className="skeleton h-3 w-1/2" />
            <div className="skeleton h-8 w-full" />
          </div>
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return <TableEmptyState title={emptyTitle} description={emptyDescription} />;
  }

  return (
    <div className="ag-mobile-record-list">
      {items.map((item) => (
        <MobileRecordCard item={item} key={item.id} />
      ))}
    </div>
  );
});
