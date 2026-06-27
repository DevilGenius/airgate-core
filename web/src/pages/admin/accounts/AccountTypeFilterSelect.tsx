import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronDown } from 'lucide-react';
import {
  renderAccountTypeFilterOption,
  type AccountTypeFilterOption,
} from './AccountPageSupport';

type AccountTypeFilterSelectProps = {
  oauthPlanOptions: AccountTypeFilterOption[];
  onOpenChange?: (isOpen: boolean) => void;
  onSelect: (value: string) => void;
  platformsLoading: boolean;
  selectedOption: AccountTypeFilterOption | undefined;
  typeOptions: AccountTypeFilterOption[];
};

export function AccountTypeFilterSelect({
  oauthPlanOptions,
  onOpenChange,
  onSelect,
  platformsLoading,
  selectedOption,
  typeOptions,
}: AccountTypeFilterSelectProps) {
  const { t } = useTranslation();
  const [isTypeMenuOpen, setIsTypeMenuOpen] = useState(false);
  const isTypeMenuOpenRef = useRef(isTypeMenuOpen);
  const menuRef = useRef<HTMLDivElement>(null);
  const selectedOnPointerDownRef = useRef(false);
  const flattenedTypeOptions = useMemo(() => [
    typeOptions[0] ?? { id: '', label: t('accounts.all_types', '全部类型') },
    ...oauthPlanOptions,
    ...typeOptions.slice(1),
  ], [oauthPlanOptions, t, typeOptions]);
  const selectedNode: ReactNode = selectedOption
    ? renderAccountTypeFilterOption(selectedOption)
    : t('accounts.all_types', '全部类型');

  useEffect(() => {
    isTypeMenuOpenRef.current = isTypeMenuOpen;
  }, [isTypeMenuOpen]);

  const setTypeMenuOpen = useCallback((nextOpen: boolean) => {
    if (isTypeMenuOpenRef.current === nextOpen) return;
    isTypeMenuOpenRef.current = nextOpen;
    onOpenChange?.(nextOpen);
    setIsTypeMenuOpen(nextOpen);
  }, [onOpenChange]);

  const closeMenu = useCallback(() => {
    setTypeMenuOpen(false);
  }, [setTypeMenuOpen]);

  const selectTypeFilter = useCallback((nextValue: string) => {
    onSelect(nextValue);
    closeMenu();
  }, [closeMenu, onSelect]);

  const toggleTypeMenu = useCallback(() => {
    setTypeMenuOpen(!isTypeMenuOpenRef.current);
  }, [setTypeMenuOpen]);

  const handleTriggerPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    toggleTypeMenu();
  }, [toggleTypeMenu]);

  const handleTriggerClick = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    if (event.detail !== 0) return;
    toggleTypeMenu();
  }, [toggleTypeMenu]);

  const handleItemPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>, value: string) => {
    selectedOnPointerDownRef.current = false;
    if (event.button !== 0) return;
    if (event.pointerType && event.pointerType !== 'mouse') return;
    event.preventDefault();
    selectedOnPointerDownRef.current = true;
    selectTypeFilter(value);
  }, [selectTypeFilter]);

  const handleItemClick = useCallback((value: string) => {
    if (selectedOnPointerDownRef.current) {
      selectedOnPointerDownRef.current = false;
      return;
    }
    selectTypeFilter(value);
  }, [selectTypeFilter]);

  useEffect(() => {
    if (!isTypeMenuOpen) return;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node && menuRef.current?.contains(target)) return;
      closeMenu();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') closeMenu();
    };

    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [closeMenu, isTypeMenuOpen]);

  return (
    <div ref={menuRef} className="select select--full-width ag-account-type-select">
      <button
        type="button"
        aria-label={t('common.type')}
        aria-haspopup="menu"
        aria-expanded={isTypeMenuOpen}
        className="select__trigger select__trigger--full-width ag-account-type-trigger"
        onClick={handleTriggerClick}
        onPointerDown={handleTriggerPointerDown}
      >
        <span className="select__value ag-account-type-trigger-value">{selectedNode}</span>
        <ChevronDown
          className="select__indicator ag-account-type-trigger-indicator"
          data-open={isTypeMenuOpen ? 'true' : undefined}
        />
      </button>
      {isTypeMenuOpen ? (
        <div className="select__popover ag-account-type-menu" role="menu">
          {flattenedTypeOptions.map((option) => (
            <button
              key={option.id}
              type="button"
              role="menuitem"
              className="ag-account-type-menu-item"
              onClick={() => handleItemClick(option.id)}
              onPointerDown={(event) => handleItemPointerDown(event, option.id)}
            >
              {renderAccountTypeFilterOption(option, true)}
            </button>
          ))}
          {oauthPlanOptions.length === 0 && platformsLoading ? (
            <span className="ag-account-type-menu-loading">{t('common.loading')}</span>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
