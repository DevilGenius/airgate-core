import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Input, Label, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { Search, User } from 'lucide-react';
import { CommonDatePicker } from '../../../shared/components/CommonDatePicker';
import { NativeCheckbox } from '../../../shared/components/NativeCheckbox';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type {
  BulkAssignReq,
  GroupResp,
  UserResp,
} from '../../../shared/types';

export function BulkAssignModal({
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
  onSubmit: (data: BulkAssignReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [selectedUserIds, setSelectedUserIds] = useState<number[]>([]);
  const [groupId, setGroupId] = useState(0);
  const [expiresAt, setExpiresAt] = useState('');
  const [userKeyword, setUserKeyword] = useState('');

  const handleClose = () => {
    setSelectedUserIds([]);
    setGroupId(0);
    setExpiresAt('');
    setUserKeyword('');
    onClose();
  };

  const toggleUser = (userId: number, selected: boolean) => {
    setSelectedUserIds((current) =>
      selected
        ? [...new Set([...current, userId])]
        : current.filter((id) => id !== userId),
    );
  };

  const handleSubmit = () => {
    if (selectedUserIds.length === 0 || !groupId || !expiresAt) return;
    onSubmit({
      expires_at: expiresAt,
      group_id: groupId,
      user_ids: selectedUserIds,
    });
  };
  const groupOptions = groups.map((group) => ({
    id: String(group.id),
    label: `${group.name} (${group.platform})`,
  }));
  const selectedGroupLabel = groupOptions.find((item) => item.id === String(groupId))?.label;
  const selectedUserIdSet = useMemo(() => new Set(selectedUserIds), [selectedUserIds]);
  const filteredUsers = useMemo(() => {
    const keyword = userKeyword.trim().toLowerCase();
    if (!keyword) return users;
    return users.filter((user) =>
      user.email.toLowerCase().includes(keyword) ||
      (user.username ?? '').toLowerCase().includes(keyword) ||
      String(user.id).includes(keyword),
    );
  }, [userKeyword, users]);
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
          <Modal.Dialog
            className="ag-elevation-modal"
            style={{ maxWidth: '560px', width: 'min(100%, calc(100vw - 2rem))' }}
          >
            <Modal.Header>
              <Modal.Heading>{t('subscriptions.bulk_assign')}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-3">
                    <Label>
                      {t('subscriptions.select_users')} <span className="text-danger">*</span>
                    </Label>
                    <span className="font-mono text-xs text-text-tertiary">
                      {t('subscriptions.selected_count', { count: selectedUserIds.length })}
                    </span>
                  </div>
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                    <Input
                      className="pl-9"
                      value={userKeyword}
                      onChange={(event) => setUserKeyword(event.target.value)}
                      placeholder={t('users.search_placeholder')}
                    />
                  </div>
                  <div className="grid max-h-56 gap-2 overflow-y-auto rounded-md border border-glass-border bg-surface p-2">
                    {filteredUsers.length === 0 ? (
                      <div className="flex min-h-20 items-center justify-center text-sm text-text-tertiary">
                        {t('common.no_data')}
                      </div>
                    ) : filteredUsers.map((user) => {
                      const isSelected = selectedUserIdSet.has(user.id);
                      return (
                        <NativeCheckbox
                          key={user.id}
                          className={`w-full rounded-md border p-2.5 transition-colors ${
                            isSelected
                              ? 'border-primary bg-primary-subtle'
                              : 'border-border-subtle bg-bg-surface hover:bg-bg-hover'
                          }`}
                          isSelected={isSelected}
                          onChange={(selected) => toggleUser(user.id, selected)}
                        >
                          <span className="flex min-w-0 items-center gap-2">
                            <User className={isSelected ? 'h-3.5 w-3.5 shrink-0 text-primary' : 'h-3.5 w-3.5 shrink-0 text-text-tertiary'} />
                            <span className="min-w-0 text-left">
                              <span className="block truncate text-sm font-medium text-text">{user.email}</span>
                              <span className="block truncate text-xs text-text-tertiary">{user.username || '-'}</span>
                            </span>
                          </span>
                        </NativeCheckbox>
                      );
                    })}
                  </div>
                </div>

                <div className="space-y-1.5">
                  <Label>{t('subscriptions.group')}</Label>
                  <SimpleSelect
                    ariaLabel={t('subscriptions.group')}
                  fullWidth
                    items={groupOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={groupId ? String(groupId) : null}
                    selectedLabel={selectedGroupLabel ?? <span className="text-text-tertiary">{t('subscriptions.select_group')}</span>}
                    onSelectionChange={(key) => setGroupId(Number(key))}
                  />
                </div>

                <CommonDatePicker
                  isRequired
                  label={t('subscriptions.expire_time')}
                  value={expiresAt ? expiresAt.split('T')[0] : ''}
                  onChange={(value) => setExpiresAt(value ? `${value}T23:59:59Z` : '')}
                />
              </div>
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={handleClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" isDisabled={loading} onPress={handleSubmit}>
                {loading ? <Spinner size="sm" /> : null}
                {t('subscriptions.bulk_assign_count', { count: selectedUserIds.length })}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
