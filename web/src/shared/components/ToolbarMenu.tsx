import { memo, useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { Check, ChevronDown } from 'lucide-react';

interface ToolbarMenuProps {
  ariaLabel: string;
  children: (close: () => void) => ReactNode;
  className?: string;
  disabled?: boolean;
  icon?: ReactNode;
  label: ReactNode;
  rootClassName?: string;
}

interface ToolbarMenuItemProps {
  children: ReactNode;
  className?: string;
  isDisabled?: boolean;
  isSelected?: boolean;
  onSelect: () => void;
  role?: 'menuitem' | 'menuitemcheckbox' | 'menuitemradio';
}

export const ToolbarMenu = memo(function ToolbarMenu({
  ariaLabel,
  children,
  className,
  disabled = false,
  icon,
  label,
  rootClassName,
}: ToolbarMenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  const close = useCallback(() => setIsOpen(false), []);
  const toggleOpen = useCallback(() => {
    flushSync(() => {
      setIsOpen((open) => !open);
    });
  }, []);

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
        <div className="ag-toolbar-menu-popover" role="presentation">
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
}: ToolbarMenuItemProps) {
  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (isDisabled || event.button !== 0) return;
    event.preventDefault();
    onSelect();
  }, [isDisabled, onSelect]);

  const handleClick = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    if (isDisabled || event.detail !== 0) return;
    onSelect();
  }, [isDisabled, onSelect]);

  return (
    <button
      type="button"
      aria-checked={role === 'menuitem' ? undefined : isSelected}
      aria-disabled={isDisabled || undefined}
      className={['ag-toolbar-menu-item', className].filter(Boolean).join(' ')}
      disabled={isDisabled}
      role={role}
      onClick={handleClick}
      onPointerDown={handlePointerDown}
    >
      <span className="ag-toolbar-menu-item-label">{children}</span>
      <span className="ag-toolbar-menu-item-check">
        {isSelected ? <Check className="h-3.5 w-3.5 text-primary" aria-hidden="true" /> : null}
      </span>
    </button>
  );
});
