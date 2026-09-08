import { memo, useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { useFloatingPopover } from '../hooks/useFloatingPopover';

interface ToolbarMenuProps {
  ariaLabel: string;
  children: (close: () => void) => ReactNode;
  className?: string;
  disabled?: boolean;
  icon?: ReactNode;
  label: ReactNode;
  onOpenChange?: (isOpen: boolean) => void;
  popoverAlign?: 'start' | 'end';
  rootClassName?: string;
}

interface ToolbarMenuItemProps {
  children: ReactNode;
  className?: string;
  isDisabled?: boolean;
  isSelected?: boolean;
  onSelect: () => void;
  role?: 'menuitem' | 'menuitemcheckbox' | 'menuitemradio';
  /** 为 false 时不渲染右侧勾选列（选中态可改用背景高亮表达）。 */
  showCheckIndicator?: boolean;
}

export const ToolbarMenu = memo(function ToolbarMenu({
  ariaLabel,
  children,
  className,
  disabled = false,
  icon,
  label,
  onOpenChange,
  popoverAlign = 'end',
  rootClassName,
}: ToolbarMenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const isOpenRef = useRef(isOpen);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const { popoverRef, triggerRef } = useFloatingPopover<HTMLDivElement>({ isOpen, align: popoverAlign });

  useEffect(() => {
    isOpenRef.current = isOpen;
  }, [isOpen]);

  const setOpen = useCallback((nextOpen: boolean) => {
    if (isOpenRef.current === nextOpen) return;
    isOpenRef.current = nextOpen;
    onOpenChange?.(nextOpen);
    setIsOpen(nextOpen);
  }, [onOpenChange]);

  const close = useCallback(() => setOpen(false), [setOpen]);
  const toggleOpen = useCallback(() => {
    setOpen(!isOpenRef.current);
  }, [setOpen]);

  const handleTriggerPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (disabled || event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    toggleOpen();
  }, [disabled, toggleOpen]);

  const handleTriggerClick = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    if (disabled || event.detail !== 0) return;
    toggleOpen();
  }, [disabled, toggleOpen]);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };

    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [close, isOpen]);

  return (
    <div ref={rootRef} className={['ag-toolbar-menu', rootClassName].filter(Boolean).join(' ')}>
      <button
        ref={triggerRef as RefObject<HTMLButtonElement | null>}
        type="button"
        aria-expanded={isOpen}
        aria-haspopup="menu"
        aria-label={ariaLabel}
        className={['ag-toolbar-menu-trigger', className].filter(Boolean).join(' ')}
        data-open={isOpen ? 'true' : undefined}
        disabled={disabled}
        onClick={handleTriggerClick}
        onPointerDown={handleTriggerPointerDown}
      >
        {icon}
        <span className="ag-toolbar-menu-trigger-label">{label}</span>
        <ChevronDown className="ag-toolbar-menu-caret" aria-hidden="true" />
      </button>
      {isOpen ? (
        <div
          ref={popoverRef}
          className="ag-toolbar-menu-popover"
          data-floating-open="true"
          popover="manual"
          role="presentation"
        >
          <div className="ag-toolbar-menu-list" role="menu" aria-label={ariaLabel}>
            {children(close)}
          </div>
        </div>
      ) : null}
    </div>
  );
});

export const ToolbarMenuItem = memo(function ToolbarMenuItem({
  children,
  className,
  isDisabled = false,
  isSelected = false,
  onSelect,
  role = 'menuitem',
  showCheckIndicator = true,
}: ToolbarMenuItemProps) {
  const selectedOnPointerDownRef = useRef(false);

  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    selectedOnPointerDownRef.current = false;
    if (isDisabled || event.button !== 0) return;
    if (event.pointerType && event.pointerType !== 'mouse') return;
    event.preventDefault();
    selectedOnPointerDownRef.current = true;
    onSelect();
  }, [isDisabled, onSelect]);

  const handleClick = useCallback(() => {
    if (isDisabled) return;
    if (selectedOnPointerDownRef.current) {
      selectedOnPointerDownRef.current = false;
      return;
    }
    onSelect();
  }, [isDisabled, onSelect]);

  return (
    <button
      type="button"
      aria-checked={role === 'menuitem' ? undefined : isSelected}
      aria-disabled={isDisabled || undefined}
      className={[
        'ag-toolbar-menu-item',
        !showCheckIndicator && 'ag-toolbar-menu-item--no-check',
        className,
      ].filter(Boolean).join(' ')}
      disabled={isDisabled}
      role={role}
      onClick={handleClick}
      onPointerDown={handlePointerDown}
    >
      <span className="ag-toolbar-menu-item-label">{children}</span>
      {showCheckIndicator ? (
        <span className="ag-toolbar-menu-item-check">
          {isSelected ? <Check className="h-3.5 w-3.5 text-primary" aria-hidden="true" /> : null}
        </span>
      ) : null}
    </button>
  );
});
