import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Label, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { CommonDatePicker } from '../../../shared/components/CommonDatePicker';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type {
  SubscriptionResp,
  AdjustSubscriptionReq,
} from '../../../shared/types';

export function AdjustModal({
  open,
  subscription,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  subscription: SubscriptionResp;
  onClose: () => void;
  onSubmit: (data: AdjustSubscriptionReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AdjustSubscriptionReq>({
    expires_at: subscription.expires_at,
    status: subscription.status as 'active' | 'suspended',
  });
  const statusOptions = [
    { id: 'active', label: t('subscriptions.status_active') },
    { id: 'suspended', label: t('subscriptions.status_suspended') },
  ];
  const selectedStatusLabel = statusOptions.find((item) => item.id === (form.status ?? 'active'))?.label ?? t('subscriptions.status_active');
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  return (
    <Modal state={modalState}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog className="ag-elevation-modal">
            <Modal.Header>
              <Modal.Heading>{t('subscriptions.adjust_title', { name: subscription.group_name })}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <CommonDatePicker
                  label={t('subscriptions.expire_time')}
                  value={form.expires_at ? form.expires_at.split('T')[0] : ''}
                  onChange={(value) => setForm({ ...form, expires_at: value ? `${value}T23:59:59Z` : undefined })}
                />

                <div className="space-y-1.5">
                  <Label>{t('common.status')}</Label>
                  <SimpleSelect
                    ariaLabel={t('common.status')}
                  fullWidth
                    items={statusOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={form.status ?? 'active'}
                    selectedLabel={selectedStatusLabel}
                  onSelectionChange={(key) =>
                    setForm({ ...form, status: (key || 'active') as 'active' | 'suspended' })
                  }
                  />
                </div>
              </div>
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={onClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" isDisabled={loading} onPress={() => onSubmit(form)}>
                {loading ? <Spinner size="sm" /> : null}
                {t('common.save')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
