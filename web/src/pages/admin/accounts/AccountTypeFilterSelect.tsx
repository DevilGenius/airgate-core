import { useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronDown, ChevronRight } from 'lucide-react';
import {
  renderAccountTypeFilterOption,
  type AccountTypeFilterOption,
} from './AccountPageSupport';

const OAUTH_SUBMENU_LONG_PRESS_MS = 450;

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
  const [isOAuthPlanMenuOpen, setIsOAuthPlanMenuOpen] = useState(false);
  const isTypeMenuOpenRef = useRef(isTypeMenuOpen);
  const menuRef = useRef<HTMLDivElement>(null);
  const selectedOnPointerDownRef = useRef(false);
  const oauthLongPressTimerRef = useRef<number | null>(null);
  const selectedNode: ReactNode = selectedOption
    ? renderAccountTypeFilterOption(selectedOption)
    : t('accounts.all_types', '全部类型');

  const clearOAuthLongPressTimer = useCallback(() => {
    if (oauthLongPressTimerRef.current === null || typeof window === 'undefined') return;
    window.clearTimeout(oauthLongPressTimerRef.current);
    oauthLongPressTimerRef.current = null;
  }, []);

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
    clearOAuthLongPressTimer();
    setIsOAuthPlanMenuOpen(false);
    setTypeMenuOpen(false);
  }, [clearOAuthLongPressTimer, setTypeMenuOpen]);

  const selectTypeFilter = useCallback((nextValue: string) => {
    onSelect(nextValue);
    closeMenu();
  }, [closeMenu, onSelect]);

  const toggleTypeMenu = useCallback(() => {
    const nextOpen = !isTypeMenuOpenRef.current;
    if (!nextOpen) setIsOAuthPlanMenuOpen(false);
    setTypeMenuOpen(nextOpen);
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

  const handleOAuthPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    selectedOnPointerDownRef.current = false;
    if (event.button !== 0) return;
    if (!event.pointerType || event.pointerType === 'mouse') {
      handleItemPointerDown(event, 'oauth');
      return;
    }
    clearOAuthLongPressTimer();
    oauthLongPressTimerRef.current = window.setTimeout(() => {
      oauthLongPressTimerRef.current = null;
      selectedOnPointerDownRef.current = true;
      setIsOAuthPlanMenuOpen(true);
    }, OAUTH_SUBMENU_LONG_PRESS_MS);
  }, [clearOAuthLongPressTimer, handleItemPointerDown]);

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

  useEffect(() => () => clearOAuthLongPressTimer(), [clearOAuthLongPressTimer]);

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
          <button
            type="button"
            role="menuitem"
            className="ag-account-type-menu-item"
            onPointerEnter={(event) => {
              if (!event.pointerType || event.pointerType === 'mouse') setIsOAuthPlanMenuOpen(false);
            }}
            onFocus={() => setIsOAuthPlanMenuOpen(false)}
            onClick={() => handleItemClick('')}
            onPointerDown={(event) => handleItemPointerDown(event, '')}
          >
            {typeOptions[0]?.label ?? t('accounts.all_types', '全部类型')}
          </button>
          <div
            className="ag-account-type-cascade-row"
            onPointerEnter={(event) => {
              if (!event.pointerType || event.pointerType === 'mouse') setIsOAuthPlanMenuOpen(true);
            }}
            onPointerLeave={(event) => {
              if (!event.pointerType || event.pointerType === 'mouse') setIsOAuthPlanMenuOpen(false);
            }}
          >
            <button
              type="button"
              role="menuitem"
              className="ag-account-type-menu-item"
              onFocus={() => setIsOAuthPlanMenuOpen(true)}
              onContextMenu={(event) => event.preventDefault()}
              onClick={() => handleItemClick('oauth')}
              onPointerCancel={clearOAuthLongPressTimer}
              onPointerDown={handleOAuthPointerDown}
              onPointerLeave={clearOAuthLongPressTimer}
              onPointerUp={clearOAuthLongPressTimer}
            >
              <span className="truncate">OAuth</span>
              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-text-tertiary" />
            </button>
            {isOAuthPlanMenuOpen ? (
              <>
                <span aria-hidden="true" className="ag-account-type-submenu-bridge" />
                <div className="ag-account-type-submenu" role="menu">
                  {oauthPlanOptions.length > 0 ? (
                    oauthPlanOptions.map((plan) => (
                      <button
                        key={plan.id}
                        type="button"
                        role="menuitem"
                        className="ag-account-type-submenu-item"
                        onClick={() => handleItemClick(plan.id)}
                        onPointerDown={(event) => handleItemPointerDown(event, plan.id)}
                      >
                        {renderAccountTypeFilterOption(plan, false)}
                      </button>
                    ))
                  ) : platformsLoading ? (
                    <span className="ag-account-type-submenu-loading">{t('common.loading')}</span>
                  ) : (
                    <span className="ag-account-type-submenu-loading">{t('accounts.no_oauth_plans', '暂无套餐')}</span>
                  )}
                </div>
              </>
            ) : null}
          </div>
          <button
            type="button"
            role="menuitem"
            className="ag-account-type-menu-item"
            onPointerEnter={(event) => {
              if (!event.pointerType || event.pointerType === 'mouse') setIsOAuthPlanMenuOpen(false);
            }}
            onFocus={() => setIsOAuthPlanMenuOpen(false)}
            onClick={() => handleItemClick('apikey')}
            onPointerDown={(event) => handleItemPointerDown(event, 'apikey')}
          >
            API Key
          </button>
        </div>
      ) : null}
    </div>
  );
}
