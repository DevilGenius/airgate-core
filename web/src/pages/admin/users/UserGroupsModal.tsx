import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Button,
  Checkbox,
  Chip,
  Input,
  Modal,
  ScrollShadow,
  Spinner,
  Table,
  TextField as HeroTextField,
  useOverlayState,
} from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { usersApi } from '../../../shared/api/users';
import { groupsApi } from '../../../shared/api/groups';
import { useCrudMutation } from '../../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import {
  MAX_RATE_MULTIPLIER,
  RATE_MULTIPLIER_STEP,
  formatRateMultiplier,
  isValidRateMultiplierValue,
  parseRateMultiplier,
} from '../../../shared/utils/rateMultiplier';
import type { UserResp, GroupResp, UpdateUserReq } from '../../../shared/types';

interface UserGroupsModalProps {
  open: boolean;
  user: UserResp;
  onClose: () => void;
  onSaved: () => void;
}

function initialRateState(groupRates?: Record<number, number>): Record<number, string> {
  const out: Record<number, string> = {};
  if (!groupRates) return out;
  for (const [key, value] of Object.entries(groupRates)) {
    if (isValidRateMultiplierValue(value)) {
      out[Number(key)] = String(value);
    }
  }
  return out;
}

export function UserGroupsModal({ open, user, onClose, onSaved }: UserGroupsModalProps) {
  const { t } = useTranslation();
  const [selectedIds, setSelectedIds] = useState<number[]>(user.allowed_group_ids ?? []);
  const [customRates, setCustomRates] = useState<Record<number, string>>(() => initialRateState(user.group_rates));

  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
    enabled: open,
  });

  const allGroups: GroupResp[] = groupsData?.list ?? [];
  const tableGroups = useMemo(
    () =>
      [...allGroups].sort((left, right) => {
        if (left.is_exclusive !== right.is_exclusive) {
          return left.is_exclusive ? 1 : -1;
        }
        return left.name.localeCompare(right.name);
      }),
    [allGroups],
  );
  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  const buildPayload = (): UpdateUserReq => {
    const group_rates: Record<number, number> = {};
    for (const [key, raw] of Object.entries(customRates)) {
      if (raw === '' || raw == null) continue;
      const value = parseRateMultiplier(raw);
      if (!isValidRateMultiplierValue(value)) continue;
      group_rates[Number(key)] = value;
    }
    return {
      allowed_group_ids: selectedIds,
      group_rates,
    };
  };

  const updateMutation = useCrudMutation({
    mutationFn: (_?: void) => usersApi.update(user.id, buildPayload()),
    successMessage: t('users.update_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => onSaved(),
  });

  const hasInvalidRate = useMemo(() => {
    for (const raw of Object.values(customRates)) {
      if (raw === '' || raw == null) continue;
      const value = parseRateMultiplier(raw);
      if (!isValidRateMultiplierValue(value)) return true;
    }
    return false;
  }, [customRates]);

  const toggleExclusiveGroup = (groupId: number, isSelected: boolean) => {
    setSelectedIds((current) =>
      isSelected
        ? [...new Set([...current, groupId])]
        : current.filter((value) => value !== groupId),
    );
  };

  const renderRateField = (group: GroupResp, enabled: boolean) => (
    <div className="w-24">
      <HeroTextField fullWidth isDisabled={!enabled}>
        <Input
          aria-label={`${group.name} ${t('groups.rate_multiplier')}`}
          type="number"
          min="0"
          max={MAX_RATE_MULTIPLIER}
          step={RATE_MULTIPLIER_STEP}
          disabled={!enabled}
          value={customRates[group.id] ?? ''}
          placeholder={formatRateMultiplier(group.rate_multiplier ?? 1)}
          onChange={(event) => setCustomRates((prev) => ({ ...prev, [group.id]: event.target.value }))}
        />
      </HeroTextField>
    </div>
  );

  const renderGroupRow = (group: GroupResp) => {
    const selected = !group.is_exclusive || selectedIdSet.has(group.id);
    const rateEnabled = !group.is_exclusive || selected;

    return (
      <Table.Row
        id={String(group.id)}
        key={group.id}
        className={selected ? 'bg-primary-subtle/55' : undefined}
      >
        <Table.Cell className="h-10 w-12 border-b border-border-subtle px-2 py-1 text-center">
          <Checkbox
            aria-label={`${group.name} ${t('common.select', '选择')}`}
            isDisabled={!group.is_exclusive}
            isSelected={selected}
            onChange={(nextSelected) => toggleExclusiveGroup(group.id, nextSelected)}
          >
            <Checkbox.Control className={selected ? 'border-primary bg-primary text-primary-foreground' : undefined}>
              <Checkbox.Indicator />
            </Checkbox.Control>
          </Checkbox>
        </Table.Cell>
        <Table.Cell className="h-10 w-[11rem] border-b border-border-subtle px-2 py-1.5">
          <span className="block min-w-0 truncate text-sm font-medium text-text" title={group.name}>
            {group.name}
          </span>
        </Table.Cell>
        <Table.Cell className="h-10 w-24 border-b border-border-subtle px-2 py-1.5">
          <span className="block max-w-[5.5rem] truncate text-xs text-text-secondary" title={group.platform}>
            {group.platform}
          </span>
        </Table.Cell>
        <Table.Cell className="h-10 w-20 border-b border-border-subtle px-2 py-1.5">
          <Chip color={group.is_exclusive ? 'warning' : 'default'} size="sm" variant="soft">
            {group.is_exclusive ? t('groups.type_exclusive') : t('groups.type_public')}
          </Chip>
        </Table.Cell>
        <Table.Cell className="h-10 w-32 border-b border-border-subtle px-2 py-1">
          {renderRateField(group, rateEnabled)}
        </Table.Cell>
      </Table.Row>
    );
  };

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
          <Modal.Dialog
            className="ag-elevation-modal"
            style={{
              maxHeight: 'min(92dvh, 56rem)',
              maxWidth: '700px',
              width: 'min(100%, calc(100vw - 2rem))',
            }}
          >
            <Modal.Header>
              <Modal.Heading>{`${t('users.groups')} - ${user.email}`}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body className="min-h-0">
              {groupsLoading ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.loading')}</p>
              ) : tableGroups.length === 0 ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.no_data')}</p>
              ) : (
                <div className="flex min-h-0 flex-col gap-3">
                  <p className="text-[12px] leading-5 text-text-tertiary">{t('users.group_rate_hint')}</p>
                  <ScrollShadow className="max-h-[74dvh] overflow-y-auto overflow-x-hidden pr-1" size={28}>
                    <Table className="bg-transparent shadow-none">
                      <Table.ScrollContainer className="overflow-x-hidden bg-transparent shadow-none">
                        <Table.Content
                          aria-label={t('users.groups')}
                          className="w-full table-fixed border-separate border-spacing-0 bg-transparent"
                        >
                          <Table.Header className="sticky top-0 z-10">
                            <Table.Column
                              id="enabled"
                              className="h-8 w-12 border-b border-border bg-surface-secondary px-2 py-1 text-center text-xs font-semibold text-text-tertiary"
                            >
                              {t('common.select', '选择')}
                            </Table.Column>
                            <Table.Column
                              id="name"
                              isRowHeader
                              className="h-8 w-[11rem] border-b border-border bg-surface-secondary px-2 py-1 text-left text-xs font-semibold text-text-tertiary"
                            >
                              {t('groups.group', '分组')}
                            </Table.Column>
                            <Table.Column
                              id="platform"
                              className="h-8 w-24 border-b border-border bg-surface-secondary px-2 py-1 text-left text-xs font-semibold text-text-tertiary"
                            >
                              {t('groups.platform')}
                            </Table.Column>
                            <Table.Column
                              id="type"
                              className="h-8 w-20 border-b border-border bg-surface-secondary px-2 py-1 text-left text-xs font-semibold text-text-tertiary"
                            >
                              {t('common.type')}
                            </Table.Column>
                            <Table.Column
                              id="rate"
                              className="h-8 w-32 border-b border-border bg-surface-secondary px-2 py-1 text-left text-xs font-semibold text-text-tertiary"
                            >
                              {t('groups.rate_multiplier')}
                            </Table.Column>
                          </Table.Header>
                          <Table.Body>{tableGroups.map(renderGroupRow)}</Table.Body>
                        </Table.Content>
                      </Table.ScrollContainer>
                    </Table>
                  </ScrollShadow>
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                variant="primary"
                isDisabled={hasInvalidRate || updateMutation.isPending}
                onPress={() => updateMutation.mutate()}
              >
                {updateMutation.isPending ? <Spinner size="sm" /> : null}
                {t('common.save')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
