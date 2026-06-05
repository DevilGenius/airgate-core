import { memo, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
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

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: MouseEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node) || root.contains(event.target)) return;
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
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
        onClick={() => setIsOpen((open) => !open)}
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
  return (
    <button
      type="button"
      aria-checked={role === 'menuitem' ? undefined : isSelected}
      aria-disabled={isDisabled || undefined}
      className={['ag-toolbar-menu-item', className].filter(Boolean).join(' ')}
      disabled={isDisabled}
      role={role}
      onClick={onSelect}
    >
      <span className="ag-toolbar-menu-item-label">{children}</span>
      <span className="ag-toolbar-menu-item-check">
        {isSelected ? <Check className="h-3.5 w-3.5 text-primary" aria-hidden="true" /> : null}
      </span>
    </button>
  );
});
