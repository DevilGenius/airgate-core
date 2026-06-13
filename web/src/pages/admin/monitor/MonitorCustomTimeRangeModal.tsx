import { useEffect, useState } from 'react';
import { Button, useOverlayState } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import { CommonModal } from '../../../shared/components/CommonModal';
import { useToast } from '../../../shared/ui';
import { formatDateTimeInputValue, localDateTimeInputToISOString } from './timeRange';

export function MonitorCustomTimeRangeModal({
  from,
  isOpen,
  onApply,
  onClear,
  onClose,
  to,
}: {
  from?: string;
  isOpen: boolean;
  onApply: (from?: string, to?: string) => void;
  onClear: () => void;
  onClose: () => void;
  to?: string;
}) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const [draftFrom, setDraftFrom] = useState('');
  const [draftTo, setDraftTo] = useState('');
  const modalState = useOverlayState({
    isOpen,
    onOpenChange: (open) => {
      if (!open) onClose();
    },
  });

  useEffect(() => {
    if (!isOpen) return;
    setDraftFrom(formatDateTimeInputValue(from));
    setDraftTo(formatDateTimeInputValue(to));
  }, [from, isOpen, to]);

  const handleApply = () => {
    const nextFrom = localDateTimeInputToISOString(draftFrom);
    const nextTo = localDateTimeInputToISOString(draftTo);
    if ((draftFrom && !nextFrom) || (draftTo && !nextTo)) {
      toast('error', t('monitor.time_range_invalid'));
      return;
    }
    if (nextFrom && nextTo && new Date(nextFrom).getTime() > new Date(nextTo).getTime()) {
      toast('error', t('monitor.time_range_order_invalid'));
      return;
    }
    onApply(nextFrom, nextTo);
  };

  return (
    <CommonModal
      className="ag-monitor-time-range-modal"
      footer={(
        <>
          <Button variant="ghost" onPress={onClear}>
            {t('common.clear')}
          </Button>
          <Button variant="secondary" onPress={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" onPress={handleApply}>
            {t('common.confirm')}
          </Button>
        </>
      )}
      size="sm"
      state={modalState}
      surface={false}
      title={t('monitor.time_range_custom_title')}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="grid gap-1.5 text-xs font-medium text-text-secondary">
          <span>{t('monitor.time_range_start')}</span>
          <input
            className="input input--sm w-full"
            step={1}
            type="datetime-local"
            value={draftFrom}
            onChange={(event) => setDraftFrom(event.target.value)}
          />
        </label>
        <label className="grid gap-1.5 text-xs font-medium text-text-secondary">
          <span>{t('monitor.time_range_end')}</span>
          <input
            className="input input--sm w-full"
            step={1}
            type="datetime-local"
            value={draftTo}
            onChange={(event) => setDraftTo(event.target.value)}
          />
        </label>
      </div>
    </CommonModal>
  );
}
