import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Description, Input, Label, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { CommonDatePicker } from '../../../shared/components/CommonDatePicker';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import { CommonModal } from '../../../shared/components/CommonModal';
import {
  MAX_RATE_MULTIPLIER,
  RATE_MULTIPLIER_STEP,
  isValidSellRateInput,
} from '../../../shared/utils/rateMultiplier';
import type { KeyForm } from './types';

export interface KeyGroupOption {
  value: string;
  label: string;
  suffix?: ReactNode;
}

export function EditKeyModal({
  open,
  isEdit,
  form,
  setForm,
  groupOptions,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  isEdit: boolean;
  form: KeyForm;
  setForm: (form: KeyForm) => void;
  groupOptions: KeyGroupOption[];
  onClose: () => void;
  onSubmit: () => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const sellRateValid = form.sell_rate.trim() === '' || isValidSellRateInput(form.sell_rate);
  const selectedGroup = groupOptions.find((option) => option.value === form.group_id);
  const groupItems = groupOptions.map((option) => ({
    id: option.value,
    label: (
      <div className="flex min-w-0 items-center justify-between gap-2">
        <span className="truncate">{option.label}</span>
        {option.suffix ? <span className="shrink-0 text-xs">{option.suffix}</span> : null}
      </div>
    ),
    textValue: option.label,
  }));
  const selectedGroupLabel = selectedGroup ? (
    <div className="flex min-w-0 items-center justify-between gap-2">
      <span className="truncate">{selectedGroup.label}</span>
      {selectedGroup.suffix ? <span className="shrink-0 text-xs">{selectedGroup.suffix}</span> : null}
    </div>
  ) : t('user_keys.select_group');
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  return (
    <CommonModal
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isDisabled={loading || !sellRateValid} onPress={onSubmit}>
            {loading ? <Spinner size="sm" /> : null}
            {isEdit ? t('common.save') : t('common.create')}
          </Button>
        </div>
      )}
      state={modalState}
      title={isEdit ? t('user_keys.edit') : t('user_keys.create')}
    >
      <div className="space-y-4">
        <HeroTextField fullWidth isRequired>
          <Label>{t('common.name')}</Label>
          <Input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder={t('user_keys.name_placeholder')}
            required
          />
        </HeroTextField>
        <div className="space-y-1.5">
          <Label>{t('user_keys.group')}</Label>
          <SimpleSelect
            ariaLabel={t('user_keys.group')}
          fullWidth
            items={groupItems.map((item) => ({ key: item.id, label: item.label }))}
          selectedKey={form.group_id || null}
            selectedLabel={selectedGroupLabel}
            onSelectionChange={(key) => setForm({ ...form, group_id: key })}
          />
        </div>
        <HeroTextField fullWidth>
          <Label>{t('user_keys.quota_label')}</Label>
          <Input
            type="number"
            value={form.quota_usd}
            onChange={(e) => setForm({ ...form, quota_usd: e.target.value })}
            placeholder={t('user_keys.quota_unlimited_hint')}
          />
          <Description>{t('user_keys.quota_hint')}</Description>
        </HeroTextField>
        <HeroTextField fullWidth>
          <Label>{t('user_keys.sell_rate_label', '销售倍率（对外售价）')}</Label>
          <Input
            type="number"
            step={RATE_MULTIPLIER_STEP}
            min="0"
            max={MAX_RATE_MULTIPLIER}
            value={form.sell_rate}
            onChange={(e) => setForm({ ...form, sell_rate: e.target.value })}
            placeholder="1"
          />
          <Description>{t('user_keys.sell_rate_hint', '1.2 表示加价 20%，1 表示不加价，0 表示客户侧免费，最大 100')}</Description>
        </HeroTextField>
        <HeroTextField fullWidth>
          <Label>{t('user_keys.max_concurrency_label', '最大并发数')}</Label>
          <Input
            type="number"
            value={form.max_concurrency}
            onChange={(e) => setForm({ ...form, max_concurrency: e.target.value })}
            placeholder="0"
          />
          <Description>{t('user_keys.max_concurrency_hint', '留空或 0 表示不限制')}</Description>
        </HeroTextField>
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0">
            <div className="text-sm font-medium text-text">{t('user_keys.balance_alert_enabled')}</div>
            <p className="mt-0.5 text-xs text-text-tertiary">{t('user_keys.balance_alert_hint')}</p>
          </div>
          <NativeSwitch
            ariaLabel={t('user_keys.balance_alert_enabled')}
            isSelected={form.balance_alert_enabled}
            onChange={(value) => setForm({ ...form, balance_alert_enabled: value })}
          />
        </div>
        {form.balance_alert_enabled && (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <HeroTextField fullWidth>
              <Label>{t('user_keys.balance_alert_email')}</Label>
              <Input
                type="email"
                value={form.balance_alert_email}
                onChange={(e) => setForm({ ...form, balance_alert_email: e.target.value })}
                placeholder="name@example.com"
              />
            </HeroTextField>
            <HeroTextField fullWidth>
              <Label>{t('user_keys.balance_alert_threshold')}</Label>
              <Input
                type="number"
                step="0.01"
                min="0"
                value={form.balance_alert_threshold}
                onChange={(e) => setForm({ ...form, balance_alert_threshold: e.target.value })}
                placeholder="5.00"
              />
            </HeroTextField>
          </div>
        )}
        <CommonDatePicker
          description={t('user_keys.expire_hint')}
          label={t('user_keys.expires_at')}
          value={form.expires_at}
          onChange={(value) => setForm({ ...form, expires_at: value })}
        />
      </div>
    </CommonModal>
  );
}
