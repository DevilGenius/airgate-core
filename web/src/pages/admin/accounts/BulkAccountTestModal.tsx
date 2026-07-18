import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Chip, Label, useOverlayState } from '@heroui/react';
import { Check, Loader2, Play, RotateCcw, X } from 'lucide-react';
import { accountsApi } from '../../../shared/api/accounts';
import { CommonModal } from '../../../shared/components/CommonModal';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type { AccountResp, ModelInfo } from '../../../shared/types';
import {
  filterConnectivityTestModels,
  runAccountConnectivityTest,
} from './accountTestRunner';

type ItemStatus = 'pending' | 'running' | 'success' | 'error';

interface ItemState {
  groupKey: string;
  id: number;
  name: string;
  status: ItemStatus;
  error?: string;
}

interface AccountTestGroup {
  key: string;
  platform: string;
  type: string;
  accounts: AccountResp[];
}

interface PlatformModelState {
  models: ModelInfo[];
  selectedModel: string;
  loading: boolean;
  error: string;
}

export function BulkAccountTestModal({
  accounts,
  onClose,
  open,
}: {
  accounts: AccountResp[];
  onClose: () => void;
  open: boolean;
}) {
  const { t } = useTranslation();
  const groups = useMemo(() => groupSimilarAccounts(accounts), [accounts]);
  const [groupModels, setGroupModels] = useState<Record<string, PlatformModelState>>({});
  const [items, setItems] = useState<ItemState[]>([]);
  const [done, setDone] = useState(0);
  const [success, setSuccess] = useState(0);
  const [failed, setFailed] = useState(0);
  const [running, setRunning] = useState(false);
  const [finished, setFinished] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!open) return;
    abortRef.current?.abort();
    setItems(accounts.map((account) => ({
      groupKey: accountTestGroupKey(account),
      id: account.id,
      name: account.name,
      status: 'pending',
    })));
    setDone(0);
    setSuccess(0);
    setFailed(0);
    setRunning(false);
    setFinished(false);
    setGroupModels(Object.fromEntries(groups.map((group) => [group.key, {
      models: [],
      selectedModel: '',
      loading: true,
      error: '',
    }])));

    let active = true;
    for (const group of groups) {
      const representative = group.accounts[0];
      if (!representative) continue;
      void accountsApi.models(representative.id)
        .then((models) => {
          if (!active) return;
          const options = filterConnectivityTestModels(models);
          setGroupModels((previous) => ({
            ...previous,
            [group.key]: {
              models: options,
              selectedModel: options[0]?.id ?? '',
              loading: false,
              error: options.length > 0 ? '' : t('accounts.bulk_test_models_error'),
            },
          }));
        })
        .catch(() => {
          if (!active) return;
          setGroupModels((previous) => ({
            ...previous,
            [group.key]: {
              models: [],
              selectedModel: '',
              loading: false,
              error: t('accounts.bulk_test_models_error'),
            },
          }));
        });
    }

    return () => {
      active = false;
    };
  }, [accounts, groups, open, t]);

  useEffect(() => () => {
    abortRef.current?.abort();
  }, []);

  const setGroupModel = useCallback((groupKey: string, model: string) => {
    setGroupModels((previous) => {
      const current = previous[groupKey];
      if (!current) return previous;
      return {
        ...previous,
        [groupKey]: { ...current, selectedModel: model },
      };
    });
  }, []);

  const recordResult = useCallback((accountId: number, result: { success: boolean; error?: string }) => {
    setItems((previous) => previous.map((item) => (
      item.id === accountId
        ? { ...item, status: result.success ? 'success' : 'error', error: result.error }
        : item
    )));
    setDone((value) => value + 1);
    if (result.success) setSuccess((value) => value + 1);
    else setFailed((value) => value + 1);
  }, []);

  const canStart = groups.length > 0 && groups.every((group) => {
    const state = groupModels[group.key];
    return state && !state.loading && Boolean(state.selectedModel);
  });

  const startTests = useCallback(async () => {
    if (!canStart || running) return;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setItems(accounts.map((account) => ({
      groupKey: accountTestGroupKey(account),
      id: account.id,
      name: account.name,
      status: 'running',
    })));
    setDone(0);
    setSuccess(0);
    setFailed(0);
    setFinished(false);
    setRunning(true);

    await Promise.all(accounts.map(async (account) => {
      const modelId = groupModels[accountTestGroupKey(account)]?.selectedModel ?? '';
      try {
        const result = await runAccountConnectivityTest({
          accountId: account.id,
          fallbackError: t('accounts.test_error'),
          modelId,
          signal: controller.signal,
        });
        if (!controller.signal.aborted) recordResult(account.id, result);
      } catch (error) {
        if ((error as Error).name === 'AbortError' || controller.signal.aborted) return;
        recordResult(account.id, { success: false, error: (error as Error).message });
      }
    }));

    if (!controller.signal.aborted) {
      setRunning(false);
      setFinished(true);
    }
  }, [accounts, canStart, groupModels, recordResult, running, t]);

  const handleClose = useCallback(() => {
    abortRef.current?.abort();
    onClose();
  }, [onClose]);
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });
  const progress = accounts.length > 0 ? Math.round((done / accounts.length) * 100) : 0;

  return (
    <CommonModal
      className="ag-account-page-modal"
      dialogStyle={{ maxWidth: '680px', width: 'min(100%, calc(100vw - 2rem))' }}
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={handleClose}>
            {running ? t('common.cancel') : t('common.close')}
          </Button>
          <Button
            aria-busy={running}
            isDisabled={!canStart || running}
            variant={finished && failed > 0 ? 'danger' : 'primary'}
            onPress={startTests}
          >
            {finished ? <RotateCcw className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
            {finished ? t('accounts.retry') : t('accounts.start_test')}
          </Button>
        </div>
      )}
      icon={<Play className="size-5" />}
      size="lg"
      state={modalState}
      title={t('accounts.bulk_test_title')}
    >
      <div className="space-y-4">
        <div className="grid gap-3 md:grid-cols-2">
          {groups.map((group) => {
            const state = groupModels[group.key];
            const options = state?.loading
              ? [{ id: '', label: t('common.loading') }]
              : (state?.models ?? []).map((model) => ({ id: model.id, label: model.name || model.id }));
            const selectedLabel = options.find((option) => option.id === state?.selectedModel)?.label ?? '';
            return (
              <div
                key={group.key}
                className="space-y-2 rounded-lg border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)] p-3"
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <Chip color="accent" size="sm" variant="soft">
                      {group.platform.toUpperCase()}
                    </Chip>
                    {group.type ? (
                      <Chip color="default" size="sm" variant="soft">
                        {group.type}
                      </Chip>
                    ) : null}
                  </div>
                  <span className="shrink-0 text-xs text-[var(--ag-text-secondary)]">
                    {t('accounts.bulk_test_group_count', { count: group.accounts.length })}
                  </span>
                </div>
                <div className="space-y-1.5">
                  <Label>{t('accounts.select_model')}</Label>
                  <SimpleSelect
                    ariaLabel={`${group.platform} ${group.type} ${t('accounts.select_model')}`}
                    fullWidth
                    isDisabled={running || Boolean(state?.loading)}
                    items={options.map((option) => ({ key: option.id, label: option.label }))}
                    selectedKey={state?.selectedModel ?? ''}
                    selectedLabel={selectedLabel}
                    onSelectionChange={(model) => setGroupModel(group.key, model)}
                  />
                </div>
                {state?.error ? (
                  <div className="text-xs text-danger">{state.error}</div>
                ) : null}
              </div>
            );
          })}
        </div>

        <div>
          <div className="mb-1.5 flex items-center justify-between text-xs text-[var(--ag-text-secondary)]">
            <span>{t('accounts.bulk_test_progress', { done, total: accounts.length })}</span>
            <span className="font-mono">{progress}%</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-[var(--ag-glass-border)]">
            <div className="h-full bg-primary" style={{ width: `${progress}%` }} />
          </div>
        </div>

        <div className="flex items-center gap-4 text-xs text-[var(--ag-text-secondary)]">
          <span className="inline-flex items-center gap-1">
            <Check className="w-3.5 h-3.5 text-success" />
            {t('accounts.bulk_test_success_count', { count: success })}
          </span>
          <span className="inline-flex items-center gap-1">
            <X className="w-3.5 h-3.5 text-danger" />
            {t('accounts.bulk_test_failed_count', { count: failed })}
          </span>
        </div>

        <div className="max-h-72 overflow-y-auto rounded-lg border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)]">
          {groups.map((group) => (
            <div key={group.key}>
              <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-[var(--ag-border-subtle)] bg-[var(--ag-bg-surface)] px-3 py-2 text-xs font-medium">
                <span>{group.platform.toUpperCase()}</span>
                {group.type ? <span className="text-[var(--ag-text-secondary)]">{group.type}</span> : null}
                <span className="truncate text-[var(--ag-text-tertiary)]">
                  {groupModels[group.key]?.selectedModel ?? ''}
                </span>
              </div>
              {items.filter((item) => item.groupKey === group.key).map((item) => (
                <div
                  key={item.id}
                  className="flex items-center gap-2 border-b border-[var(--ag-border-subtle)] px-3 py-2 text-xs last:border-b-0"
                >
                  <TestStatusIcon status={item.status} />
                  <span className="min-w-0 flex-1 truncate text-[var(--ag-text)]">{item.name}</span>
                  {item.error ? (
                    <span className="max-w-[260px] truncate text-danger" title={item.error}>
                      {item.error}
                    </span>
                  ) : null}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </CommonModal>
  );
}

function groupSimilarAccounts(accounts: AccountResp[]): AccountTestGroup[] {
  const grouped = new Map<string, AccountResp[]>();
  for (const account of accounts) {
    const key = accountTestGroupKey(account);
    const group = grouped.get(key);
    if (group) group.push(account);
    else grouped.set(key, [account]);
  }
  return Array.from(grouped, ([key, groupedAccounts]) => {
    const representative = groupedAccounts[0]!;
    return {
      key,
      platform: representative.platform,
      type: representative.type ?? '',
      accounts: groupedAccounts,
    };
  });
}

function accountTestGroupKey(account: AccountResp): string {
  return `${account.platform}\u0000${account.type ?? ''}`;
}

function TestStatusIcon({ status }: { status: ItemStatus }) {
  if (status === 'running') {
    return <Loader2 className="w-3.5 h-3.5 animate-spin text-primary" />;
  }
  if (status === 'success') {
    return <Check className="w-3.5 h-3.5 text-success" />;
  }
  if (status === 'error') {
    return <X className="w-3.5 h-3.5 text-danger" />;
  }
  return <span className="h-3.5 w-3.5 rounded-full bg-[var(--ag-glass-border)]" />;
}
