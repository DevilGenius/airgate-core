import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Label, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { CommonDatePicker } from '../../../shared/components/CommonDatePicker';
import { SearchFilterComboBox } from '../../../shared/components/SearchFilterComboBox';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type {
  AssignSubscriptionReq,
  GroupResp,
  UserResp,
} from '../../../shared/types';

export function AssignModal({
  open,
  groups,
  users,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  groups: GroupResp[];
  users: UserResp[];
  onClose: () => void;
  onSubmit: (data: AssignSubscriptionReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AssignSubscriptionReq>({
    expires_at: '',
    group_id: 0,
    user_id: 0,
  });
  const [userKeyword, setUserKeyword] = useState('');
  const [selectedUserLabel, setSelectedUserLabel] = useState('');

  const handleClose = () => {
    setForm({ user_id: 0, group_id: 0, expires_at: '' });
    setUserKeyword('');
    setSelectedUserLabel('');
    onClose();
  };

  const handleUserInputChange = (value: string) => {
    if (form.user_id) {
      if (value === selectedUserLabel) return;
      setSelectedUserLabel('');
      setForm((current) => ({ ...current, user_id: 0 }));
      setUserKeyword('');
      return;
    }
    setUserKeyword(value);
  };

  const handleSubmit = () => {
    if (!form.user_id || !form.group_id || !form.expires_at) return;
    onSubmit(form);
  };
  const userOptions = useMemo(() => users.map((user) => ({
    id: String(user.id),
    label: user.email,
    description: user.username || '-',
    matchText: `${user.id} ${user.email} ${user.username ?? ''}`.toLowerCase(),
  })), [users]);
  const groupOptions = useMemo(() => groups.map((group) => ({
    id: String(group.id),
    label: `${group.name} (${group.platform})`,
  })), [groups]);
  const selectedGroupLabel = groupOptions.find((item) => item.id === String(form.group_id))?.label;
  const filteredUserOptions = useMemo(() => {
    const keyword = userKeyword.trim().toLowerCase();
    if (!keyword) return userOptions;
    return userOptions.filter((item) =>
      item.matchText.includes(keyword),
    );
  }, [userKeyword, userOptions]);
  const handleUserSelectionChange = (value: string, label: string) => {
    if (!value) {
      setForm((current) => ({ ...current, user_id: 0 }));
      setSelectedUserLabel('');
      setUserKeyword('');
      return;
    }
    const option = userOptions.find((item) => item.id === value);
    setForm((current) => ({
      ...current,
      user_id: option ? Number(option.id) : 0,
    }));
    if (option) {
      setSelectedUserLabel(label);
      setUserKeyword(label);
    }
  };
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });

  return (
    <Modal state={modalState}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog className="ag-elevation-modal">
            <Modal.Header>
              <Modal.Heading>{t('subscriptions.assign')}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <div className="space-y-1.5">
                  <Label>{t('subscriptions.user')}</Label>
                  <SearchFilterComboBox
                    ariaLabel={t('subscriptions.select_user')}
                    debounceMs={0}
                    emptyPrompt={t('users.search_placeholder')}
                    items={filteredUserOptions.map((item) => ({
                      id: item.id,
                      label: item.label,
                      description: item.description,
                      textValue: `${item.label} ${item.description}`,
                    }))}
                    noDataLabel={t('common.no_data')}
                    placeholder={t('users.search_placeholder')}
                    selectedKey={form.user_id ? String(form.user_id) : null}
                    selectedLabel={selectedUserLabel}
                    onSearchChange={handleUserInputChange}
                    onSelectionChange={handleUserSelectionChange}
                  />
                </div>

                <div className="space-y-1.5">
                  <Label>{t('subscriptions.group')}</Label>
                  <SimpleSelect
                    ariaLabel={t('subscriptions.group')}
                  fullWidth
                    items={groupOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={form.group_id ? String(form.group_id) : null}
                    selectedLabel={selectedGroupLabel ?? <span className="text-text-tertiary">{t('subscriptions.select_group')}</span>}
                    onSelectionChange={(key) => setForm({ ...form, group_id: Number(key) })}
                  />
                </div>

                <CommonDatePicker
                  isRequired
                  label={t('subscriptions.expire_time')}
                  value={form.expires_at ? form.expires_at.split('T')[0] : ''}
                  onChange={(value) => setForm({ ...form, expires_at: value ? `${value}T23:59:59Z` : '' })}
                />
              </div>
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={handleClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" isDisabled={loading} onPress={handleSubmit}>
                {loading ? <Spinner size="sm" /> : null}
                {t('subscriptions.assign')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
