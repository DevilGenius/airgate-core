import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import { flushSync } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { Input, Label, TextArea, TextField as HeroTextField } from '@heroui/react';
import { Check, ChevronDown } from 'lucide-react';
import {
  getSchemaAccountTypes,
  getSchemaSelectedAccountType,
  getSchemaVisibleFields,
} from './accountUtils';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type { CredentialField, CredentialSchemaResp } from '../../../shared/types';

// ==================== 凭证字段渲染 ====================

export function CredentialFieldInput({
  field,
  value,
  onChange,
  disabled,
  placeholder,
}: {
  field: CredentialField;
  value: string;
  onChange: (val: string) => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  const hint = placeholder ?? field.placeholder;

  if (field.type === 'textarea') {
    return (
      <HeroTextField fullWidth isDisabled={disabled} isRequired={field.required}>
        <Label>{field.label}</Label>
        <TextArea
          name={field.key}
          placeholder={hint}
          value={value}
          rows={3}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          required={field.required}
        />
      </HeroTextField>
    );
  }

  // text 和 password 都使用 Input
  // 密码字段使用 type="text" + CSS 遮蔽，避免浏览器检测到 password 字段自动填充
  const isPassword = field.type === 'password';
  return (
    <HeroTextField fullWidth isDisabled={disabled} isRequired={field.required}>
      <Label>{field.label}</Label>
      <Input
        name={field.key}
        type="text"
        placeholder={hint}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        autoComplete="off"
        required={field.required}
        style={isPassword ? { WebkitTextSecurity: 'disc', textSecurity: 'disc' } as CSSProperties : undefined}
      />
    </HeroTextField>
  );
}

export function SchemaCredentialsForm({
  schema,
  accountType,
  onAccountTypeChange,
  credentials,
  onCredentialsChange,
  mode = 'create',
}: {
  schema: CredentialSchemaResp;
  accountType: string;
  onAccountTypeChange: (type: string) => void;
  credentials: Record<string, string>;
  onCredentialsChange: (credentials: Record<string, string>) => void;
  mode?: 'create' | 'edit';
}) {
  const { t } = useTranslation();
  const accountTypes = getSchemaAccountTypes(schema);
  const selectedType = getSchemaSelectedAccountType(schema, accountType);
  const visibleFields = getSchemaVisibleFields(schema, accountType);

  return (
    <div
      className="space-y-4 pt-4"
      style={{ borderTop: '1px solid var(--ag-border)' }}
    >
      <p
        className="text-xs font-medium uppercaser"
        style={{ color: 'var(--ag-text-secondary)' }}
      >
        {t('accounts.credentials')}
      </p>

      {accountTypes.length > 0 && mode === 'create' && (
        <>
          <div className="ag-account-type-field">
            <Label>{t('common.type')}</Label>
            <SimpleSelect
              ariaLabel={t('common.type')}
              className="ag-account-type-select"
              fullWidth
              items={accountTypes.map((item) => ({ key: item.key, label: item.label }))}
              selectedKey={selectedType?.key ?? ''}
              selectedLabel={selectedType?.label ?? ''}
              triggerClassName="ag-account-type-select-trigger"
              onSelectionChange={onAccountTypeChange}
            />
          </div>
          {selectedType?.description && (
            <p className="ag-account-type-description">
              {selectedType.description}
            </p>
          )}
        </>
      )}

      {visibleFields
        .filter((field) => !(mode === 'edit' && field.edit_disabled))
        .map((field) => (
          <CredentialFieldInput
            key={field.key}
            field={field}
            value={credentials[field.key] ?? ''}
            onChange={(val) =>
              onCredentialsChange({ ...credentials, [field.key]: val })
            }
            placeholder={mode === 'edit' && field.type === 'password' ? t('accounts.leave_empty_to_keep') : undefined}
          />
        ))}
    </div>
  );
}

// ==================== 分组多选 ====================

export function GroupCheckboxList({
  groups,
  isDisabled = false,
  showLabel = true,
  selectedIds,
  onChange,
}: {
  groups: { id: number; name: string; platform: string }[];
  isDisabled?: boolean;
  showLabel?: boolean;
  selectedIds: number[];
  onChange: (ids: number[]) => void;
}) {
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);
  const [popoverStyle, setPopoverStyle] = useState<CSSProperties | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const blockNextOutsideClickRef = useRef(false);
  const outsideClickResetTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);
  const selectedOnPointerDownRef = useRef(false);

  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const selectedGroups = groups.filter((g) => selectedIdSet.has(g.id));
  const selectedLabel = selectedGroups.length === 0
    ? t('accounts.select_groups')
    : selectedGroups.map((g) => g.name).join('、');
  const close = useCallback(() => setIsOpen(false), []);
  const updatePopoverPosition = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger) return;

    const rect = trigger.getBoundingClientRect();
    const gap = 6;
    const viewportMargin = 12;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const measuredHeight = popoverRef.current?.scrollHeight ?? (groups.length * 38 + 8);
    const naturalHeight = Math.max(44, measuredHeight);
    const spaceBelow = viewportHeight - rect.bottom - gap - viewportMargin;
    const spaceAbove = rect.top - gap - viewportMargin;
    const placeAbove = spaceBelow < naturalHeight && spaceAbove > spaceBelow;
    const availableHeight = Math.max(120, placeAbove ? spaceAbove : spaceBelow);
    const maxHeight = Math.min(naturalHeight, availableHeight);
    const width = rect.width;
    const left = Math.min(
      Math.max(rect.left, viewportMargin),
      Math.max(viewportMargin, viewportWidth - width - viewportMargin),
    );
    const top = placeAbove
      ? Math.max(viewportMargin, rect.top - gap - maxHeight)
      : Math.min(rect.bottom + gap, viewportHeight - viewportMargin - maxHeight);

    setPopoverStyle({
      '--trigger-width': `${width}px`,
      left: `${left}px`,
      maxHeight: `${maxHeight}px`,
      position: 'fixed',
      top: `${top}px`,
      width: `${width}px`,
    } as CSSProperties);
  }, [groups.length]);
  const blockNextBackgroundClick = useCallback(() => {
    blockNextOutsideClickRef.current = true;
    if (outsideClickResetTimerRef.current !== null) {
      window.clearTimeout(outsideClickResetTimerRef.current);
    }
    outsideClickResetTimerRef.current = window.setTimeout(() => {
      blockNextOutsideClickRef.current = false;
      outsideClickResetTimerRef.current = null;
    }, 350);
  }, []);
  const toggleOpen = useCallback(() => {
    flushSync(() => {
      setIsOpen((open) => !open);
    });
  }, []);
  const toggleGroup = useCallback((groupId: number) => {
    if (isDisabled) return;
    const nextSelected = selectedIdSet.has(groupId)
      ? selectedIds.filter((id) => id !== groupId)
      : [...selectedIds, groupId];
    onChange(nextSelected);
  }, [isDisabled, onChange, selectedIdSet, selectedIds]);
  const handleTriggerPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>) => {
    if (isDisabled) return;
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    toggleOpen();
  }, [isDisabled, toggleOpen]);
  const handleTriggerClick = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    if (isDisabled) return;
    if (event.detail !== 0) return;
    toggleOpen();
  }, [isDisabled, toggleOpen]);
  const handleOptionPointerDown = useCallback((event: ReactPointerEvent<HTMLButtonElement>, groupId: number) => {
    selectedOnPointerDownRef.current = false;
    if (event.button !== 0) return;
    if (event.pointerType && event.pointerType !== 'mouse') return;
    event.preventDefault();
    selectedOnPointerDownRef.current = true;
    toggleGroup(groupId);
  }, [toggleGroup]);
  const handleOptionClick = useCallback((groupId: number) => {
    if (selectedOnPointerDownRef.current) {
      selectedOnPointerDownRef.current = false;
      return;
    }
    toggleGroup(groupId);
  }, [toggleGroup]);

  useEffect(() => {
    if (isDisabled && isOpen) {
      close();
    }
  }, [close, isDisabled, isOpen]);

  useEffect(() => {
    const handleBlockedClick = (event: MouseEvent) => {
      if (!blockNextOutsideClickRef.current) return;
      const targetElement = event.target instanceof Element ? event.target : null;
      if (targetElement?.closest('.ag-elevation-modal')) return;
      blockNextOutsideClickRef.current = false;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
    };

    document.addEventListener('click', handleBlockedClick, true);
    return () => {
      if (outsideClickResetTimerRef.current !== null) {
        window.clearTimeout(outsideClickResetTimerRef.current);
        outsideClickResetTimerRef.current = null;
      }
      blockNextOutsideClickRef.current = false;
      document.removeEventListener('click', handleBlockedClick, true);
    };
  }, []);

  useLayoutEffect(() => {
    if (!isOpen) {
      setPopoverStyle(undefined);
      return undefined;
    }

    updatePopoverPosition();
    const animationFrame = window.requestAnimationFrame(updatePopoverPosition);
    const visualViewport = window.visualViewport;

    window.addEventListener('resize', updatePopoverPosition);
    document.addEventListener('scroll', updatePopoverPosition, true);
    visualViewport?.addEventListener('resize', updatePopoverPosition);
    visualViewport?.addEventListener('scroll', updatePopoverPosition);
    return () => {
      window.cancelAnimationFrame(animationFrame);
      window.removeEventListener('resize', updatePopoverPosition);
      document.removeEventListener('scroll', updatePopoverPosition, true);
      visualViewport?.removeEventListener('resize', updatePopoverPosition);
      visualViewport?.removeEventListener('scroll', updatePopoverPosition);
    };
  }, [isOpen, updatePopoverPosition]);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root || !(event.target instanceof Node)) return;
      if (root.contains(event.target)) return;
      const targetElement = event.target instanceof Element ? event.target : event.target.parentElement;
      if (targetElement?.closest('.ag-elevation-modal')) {
        close();
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      blockNextBackgroundClick();
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };

    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [blockNextBackgroundClick, close, isOpen]);

  if (groups.length === 0) return null;

  return (
    <div
      ref={rootRef}
      className="select select--full-width ag-account-group-select"
      data-disabled={isDisabled ? 'true' : undefined}
      data-open={isOpen ? 'true' : undefined}
    >
      {showLabel ? (
        <Label>{t('accounts.groups')}</Label>
      ) : null}
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        aria-label={t('accounts.groups')}
        className="select__trigger select__trigger--full-width"
        data-open={isOpen ? 'true' : undefined}
        disabled={isDisabled}
        onClick={handleTriggerClick}
        onPointerDown={handleTriggerPointerDown}
      >
        <span className="select__value">
          <span className={selectedGroups.length === 0 || isDisabled ? 'block min-w-0 truncate text-text-tertiary' : 'block min-w-0 truncate'}>
            {selectedLabel}
          </span>
        </span>
        <ChevronDown className="select__indicator h-4 w-4" />
      </button>
      {isOpen ? (
        <div
          ref={popoverRef}
          className="select__popover ag-account-group-select-popover"
          role="presentation"
          style={popoverStyle ?? { visibility: 'hidden' }}
        >
          <div className="ag-account-group-select-list" role="listbox" aria-label={t('accounts.groups')} aria-multiselectable="true">
            {groups.map((group) => {
              const isSelected = selectedIdSet.has(group.id);
              return (
                <button
                  key={group.id}
                  type="button"
                  aria-selected={isSelected}
                  className="ag-account-group-select-option"
                  role="option"
                  onClick={() => handleOptionClick(group.id)}
                  onPointerDown={(event) => handleOptionPointerDown(event, group.id)}
                >
                  <span
                    aria-hidden="true"
                    className="ag-account-group-select-check"
                  >
                    {isSelected ? <Check className="h-3.5 w-3.5" /> : null}
                  </span>
                  <span className="min-w-0 truncate text-sm text-text">{group.name}</span>
                  <span className="shrink-0 rounded border border-border-subtle px-1.5 py-0 text-[10px] leading-4 text-text-tertiary">
                    {group.platform}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      ) : null}
    </div>
  );
}
