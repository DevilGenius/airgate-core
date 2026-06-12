import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Description, Input, Label, Spinner, TextArea, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { KeyRound } from 'lucide-react';
import { parseIpList } from '../../../shared/utils/ip';
import { dateInputToLocalStartRFC3339, formatDateInputValue } from '../../../shared/utils/format';
import { useAuth } from '../../../app/providers/AuthProvider';
import { CommonModal } from '../../../shared/components/CommonModal';
import { CommonDatePicker } from '../../../shared/components/CommonDatePicker';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import {
  MAX_RATE_MULTIPLIER,
  RATE_MULTIPLIER_STEP,
  formatRateMultiplier,
  isValidRateMultiplierValue,
  isValidSellRateValue,
  parseRateMultiplier,
} from '../../../shared/utils/rateMultiplier';
import type { CreateAPIKeyReq, GroupResp } from '../../../shared/types';

interface CreateKeyModalProps {
  open: boolean;
  groups: GroupResp[];
  onClose: () => void;
  onSubmit: (data: CreateAPIKeyReq) => void;
  loading: boolean;
}

const defaultForm: CreateAPIKeyReq = {
  expires_at: '',
  group_id: 0,
  max_concurrency: 0,
  name: '',
  quota_usd: 0,
};

export function CreateKeyModal({ open, groups, onClose, onSubmit, loading }: CreateKeyModalProps) {
  const { t } = useTranslation();
  const { user } = useAuth();
  const [form, setForm] = useState<CreateAPIKeyReq>(defaultForm);
  const [sellRateInput, setSellRateInput] = useState('1');
  const [ipWhitelist, setIpWhitelist] = useState('');
  const [ipBlacklist, setIpBlacklist] = useState('');

  const handleClose = () => {
    setForm(defaultForm);
    setSellRateInput('1');
    setIpWhitelist('');
    setIpBlacklist('');
    onClose();
  };

  const parsedSellRate = sellRateInput.trim() ? parseRateMultiplier(sellRateInput) : 1;
  const sellRateValid = isValidSellRateValue(parsedSellRate);

  const handleSubmit = () => {
    if (!form.name || !form.group_id) return;
    if (!sellRateValid) return;
    onSubmit({
      ...form,
      expires_at: form.expires_at || undefined,
      ip_blacklist: parseIpList(ipBlacklist),
      ip_whitelist: parseIpList(ipWhitelist),
      max_concurrency: form.max_concurrency ?? 0,
      quota_usd: form.quota_usd || undefined,
      sell_rate: parsedSellRate ?? 1,
    });
  };

  const groupOptions = groups.map((group) => {
    const override = user?.group_rates?.[group.id];
    const hasOverride = isValidRateMultiplierValue(override ?? null) && override !== group.rate_multiplier;
    return {
      id: String(group.id),
      label: (
        <div className="flex min-w-0 items-center justify-between gap-2">
          <span className="truncate">{group.name} ({group.platform})</span>
          <span className="shrink-0 text-xs text-text-tertiary">
            {hasOverride ? (
              <>
                <span className="line-through opacity-60">{formatRateMultiplier(group.rate_multiplier)}x</span>{' '}
                <span className="font-medium text-primary">{formatRateMultiplier(override)}x</span>
              </>
            ) : (
              <>{formatRateMultiplier(group.rate_multiplier)}x 倍率</>
            )}
          </span>
        </div>
      ),
      textValue: `${group.name} ${group.platform}`,
    };
  });
  const selectedGroupLabel =
    groupOptions.find((item) => item.id === String(form.group_id))?.label ?? t('api_keys.select_group');
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });

  return (
    <CommonModal
      className="ag-create-key-modal"
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={handleClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isDisabled={loading || !sellRateValid} onPress={handleSubmit}>
            {loading ? <Spinner size="sm" /> : null}
            {t('common.create')}
          </Button>
        </div>
      )}
      size="lg"
      state={modalState}
      title={t('api_keys.create')}
    >
      <div className="ag-form-scroll-safe">
        <div className="grid grid-cols-1 gap-x-8 gap-y-6 md:grid-cols-2">
          <div className="space-y-5">
            <HeroTextField fullWidth isRequired>
              <Label>{t('common.name')}</Label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                <Input
                  className="pl-9"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder={t('api_keys.name_placeholder')}
                  required
                />
              </div>
            </HeroTextField>

            <div className="space-y-1.5">
              <Label>{t('api_keys.group')}</Label>
              <SimpleSelect
                ariaLabel={t('api_keys.group')}
              fullWidth
                items={groupOptions.map((item) => ({ key: item.id, label: item.label }))}
              selectedKey={form.group_id ? String(form.group_id) : null}
                selectedLabel={selectedGroupLabel}
                onSelectionChange={(key) => setForm({ ...form, group_id: Number(key) })}
              />
            </div>

            <HeroTextField fullWidth>
              <Label>{t('api_keys.quota_label')}</Label>
              <Input
                type="number"
                step="0.01"
                min="0"
                value={String(form.quota_usd ?? 0)}
                onChange={(e) => setForm({ ...form, quota_usd: Number(e.target.value) })}
              />
              <Description>{t('api_keys.quota_hint')}</Description>
            </HeroTextField>

            <HeroTextField fullWidth>
              <Label>{t('api_keys.sell_rate_label', '销售倍率')}</Label>
              <Input
                type="number"
                step={RATE_MULTIPLIER_STEP}
                min="0"
                max={MAX_RATE_MULTIPLIER}
                value={sellRateInput}
                onChange={(e) => setSellRateInput(e.target.value)}
              />
              <Description>{t('api_keys.sell_rate_hint', '1.2 表示加价 20%，1 表示不加价，0 表示客户侧免费，最大 100')}</Description>
            </HeroTextField>
          </div>

          <div className="space-y-5">
            <HeroTextField fullWidth>
              <Label>{t('api_keys.max_concurrency_label', '最大并发数')}</Label>
              <Input
                type="number"
                step="1"
                min="0"
                value={String(form.max_concurrency ?? 0)}
                onChange={(e) => setForm({ ...form, max_concurrency: Number(e.target.value) })}
              />
              <Description>{t('api_keys.max_concurrency_hint', '留空或 0 表示不限制')}</Description>
            </HeroTextField>

            <CommonDatePicker
              description={t('api_keys.expire_hint')}
              label={t('api_keys.expire_time')}
              value={formatDateInputValue(form.expires_at)}
              onChange={(value) => setForm({ ...form, expires_at: dateInputToLocalStartRFC3339(value) })}
            />

            <HeroTextField fullWidth>
              <Label>{t('api_keys.ip_whitelist')}</Label>
              <TextArea
                className="font-mono"
                placeholder={t('api_keys.ip_placeholder')}
                value={ipWhitelist}
                onChange={(e) => setIpWhitelist(e.target.value)}
                rows={2}
              />
            </HeroTextField>

            <HeroTextField fullWidth>
              <Label>{t('api_keys.ip_blacklist')}</Label>
              <TextArea
                className="font-mono"
                placeholder={t('api_keys.ip_placeholder')}
                value={ipBlacklist}
                onChange={(e) => setIpBlacklist(e.target.value)}
                rows={2}
              />
            </HeroTextField>
          </div>
        </div>
      </div>
    </CommonModal>
  );
}
