import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Modal } from '../../../shared/components/Modal';
import { Button } from '../../../shared/components/Button';
import { pluginsApi } from '../../../shared/api/plugins';
import { accountsApi } from '../../../shared/api/accounts';
import { useToast } from '../../../shared/components/Toast';
import { queryKeys } from '../../../shared/queryKeys';
import { getPlatformPluginMap } from './accountUtils';
import { CheckCircle, XCircle, Loader2 } from 'lucide-react';

interface BatchSKImportModalProps {
  open: boolean;
  onClose: () => void;
}

interface BatchResult {
  account_type: string;
  account_name: string;
  credentials: Record<string, string>;
  status: string;
  error?: string;
}

type Phase = 'input' | 'exchanging' | 'result';

export function BatchSKImportModal({ open, onClose }: BatchSKImportModalProps) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [skText, setSKText] = useState('');
  const [phase, setPhase] = useState<Phase>('input');
  const [results, setResults] = useState<BatchResult[]>([]);
  const [importedCount, setImportedCount] = useState(0);

  function parseSessionKeys(text: string): string[] {
    return text
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line.length > 0 && !line.startsWith('#'));
  }

  const exchangeMutation = useMutation({
    mutationFn: async (sessionKeys: string[]) => {
      const map = await getPlatformPluginMap();
      const pluginId = map.get('claude');
      if (!pluginId) throw new Error(t('accounts.batch_sk_no_plugin'));

      const resp = await pluginsApi.rpc<{ results: BatchResult[] }>(
        pluginId,
        'console/batch-cookie-auth',
        { session_keys: sessionKeys },
      );
      return resp.results;
    },
    onSuccess: async (batchResults) => {
      setResults(batchResults);

      // Auto-import successful accounts
      const toImport = batchResults
        .filter((r) => r.status === 'ok' && r.credentials)
        .map((r) => ({
          name: r.account_name || 'Claude Code',
          platform: 'claude',
          type: r.account_type || 'oauth',
          credentials: r.credentials,
          priority: 50,
          max_concurrency: 10,
          rate_multiplier: 1,
        }));

      if (toImport.length > 0) {
        try {
          const importResp = await accountsApi.import(toImport);
          setImportedCount(importResp.imported);
          queryClient.invalidateQueries({ queryKey: queryKeys.accounts() });
        } catch {
          toast('error', t('accounts.batch_sk_import_failed'));
        }
      }
      setPhase('result');
    },
    onError: (err: Error) => {
      toast('error', err.message);
      setPhase('input');
    },
  });

  function handleSubmit() {
    const keys = parseSessionKeys(skText);
    if (keys.length === 0) {
      toast('error', t('accounts.batch_sk_empty'));
      return;
    }
    setPhase('exchanging');
    exchangeMutation.mutate(keys);
  }

  function handleClose() {
    setSKText('');
    setPhase('input');
    setResults([]);
    setImportedCount(0);
    onClose();
  }

  const successCount = results.filter((r) => r.status === 'ok').length;
  const failedCount = results.filter((r) => r.status === 'failed').length;

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('accounts.batch_sk_title')}
      width="560px"
      footer={
        phase === 'input' ? (
          <>
            <Button variant="secondary" onClick={handleClose}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleSubmit} loading={exchangeMutation.isPending}>
              {t('accounts.batch_sk_submit')}
            </Button>
          </>
        ) : phase === 'result' ? (
          <Button onClick={handleClose}>{t('common.close')}</Button>
        ) : undefined
      }
    >
      {phase === 'input' && (
        <div className="space-y-3">
          <p className="text-xs text-text-secondary">
            {t('accounts.batch_sk_hint')}
          </p>
          <textarea
            className="w-full h-48 px-3 py-2 text-sm rounded-lg border resize-none font-mono"
            style={{
              borderColor: 'var(--ag-border)',
              backgroundColor: 'var(--ag-bg-input)',
              color: 'var(--ag-text)',
            }}
            placeholder={t('accounts.batch_sk_placeholder')}
            value={skText}
            onChange={(e) => setSKText(e.target.value)}
          />
          <p className="text-xs text-text-tertiary">
            {t('accounts.batch_sk_count', { count: parseSessionKeys(skText).length })}
          </p>
        </div>
      )}

      {phase === 'exchanging' && (
        <div className="flex flex-col items-center justify-center py-8 gap-3">
          <Loader2 className="w-8 h-8 animate-spin text-primary" />
          <p className="text-sm text-text-secondary">
            {t('accounts.batch_sk_exchanging')}
          </p>
        </div>
      )}

      {phase === 'result' && (
        <div className="space-y-3">
          <div className="flex items-center gap-4 text-sm">
            <span className="flex items-center gap-1 text-green-600">
              <CheckCircle className="w-4 h-4" />
              {t('accounts.batch_sk_success', { count: successCount })}
            </span>
            {failedCount > 0 && (
              <span className="flex items-center gap-1 text-red-500">
                <XCircle className="w-4 h-4" />
                {t('accounts.batch_sk_failed', { count: failedCount })}
              </span>
            )}
          </div>
          {importedCount > 0 && (
            <p className="text-xs text-text-secondary">
              {t('accounts.batch_sk_imported', { count: importedCount })}
            </p>
          )}
          <div className="max-h-60 overflow-y-auto space-y-1">
            {results.map((r, i) => (
              <div
                key={i}
                className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs"
                style={{ backgroundColor: 'var(--ag-bg-hover)' }}
              >
                {r.status === 'ok' ? (
                  <CheckCircle className="w-3.5 h-3.5 text-green-600 shrink-0" />
                ) : (
                  <XCircle className="w-3.5 h-3.5 text-red-500 shrink-0" />
                )}
                <span className="truncate text-text">
                  {r.account_name || `SK #${i + 1}`}
                </span>
                {r.error && (
                  <span className="ml-auto text-red-500 truncate max-w-[200px]" title={r.error}>
                    {r.error}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}
