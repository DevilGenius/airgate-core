import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button, Label, Spinner, TextArea, TextField as HeroTextField, useOverlayState,
} from '@heroui/react';
import { Check, FileJson2, Files, FolderOpen, Trash2, X } from 'lucide-react';
import { CommonModal } from '../../../shared/components/CommonModal';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';

export type CompatImportFormat =
  | 'sub2api'
  | 'cpa'
  | 'codex'
  | 'cockpit'
  | 'agent_identity'
  | 'account_json'
  | 'refresh_token';

export type CompatImportInput = {
  name: string;
  content: string;
};

export type CompatImportProgress = {
  done: number;
  failed: number;
  success: number;
  total: number;
};

type CompatImportSelection = CompatImportFormat | 'json_input';

const MAX_COMPAT_IMPORT_INPUTS = 1024;
const MAX_COMPAT_IMPORT_MIB = 32;
const MAX_COMPAT_IMPORT_BYTES = MAX_COMPAT_IMPORT_MIB * 1024 * 1024;

function fileIdentity(file: File): string {
  return [file.webkitRelativePath || file.name, file.size, file.lastModified].join('\u0000');
}

function mergeFiles(current: File[], added: File[]): File[] {
  const merged = new Map(current.map((file) => [fileIdentity(file), file]));
  for (const file of added) merged.set(fileIdentity(file), file);
  return Array.from(merged.values());
}

export function CompatImportModal({
  open,
  loading,
  onClose,
  onSubmit,
}: {
  open: boolean;
  loading: boolean;
  onClose: () => void;
  onSubmit: (
    format: CompatImportFormat,
    inputs: CompatImportInput[],
    onProgress: (progress: CompatImportProgress) => void,
  ) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);
  const [selection, setSelection] = useState<CompatImportSelection>('sub2api');
  const [files, setFiles] = useState<File[]>([]);
  const [lineInputText, setLineInputText] = useState('');
  const [readError, setReadError] = useState('');
  const [isPreparing, setIsPreparing] = useState(false);
  const [progress, setProgress] = useState<CompatImportProgress | null>(null);
  const busy = loading || isPreparing;
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen && !busy) onClose();
    },
  });

  useEffect(() => {
    const folderInput = folderInputRef.current;
    if (!folderInput) return;
    folderInput.setAttribute('webkitdirectory', '');
    folderInput.setAttribute('directory', '');
  }, []);

  const formatOptions = useMemo(() => [
    { key: 'sub2api', label: 'Sub2API' },
    { key: 'cpa', label: 'CPA' },
    { key: 'codex', label: 'Codex Auth' },
    { key: 'cockpit', label: 'Cockpit' },
    { key: 'agent_identity', label: 'Agent Identity' },
    { key: 'account_json', label: t('accounts.compat_import_format_json_file') },
    { key: 'json_input', label: t('accounts.compat_import_format_json_input') },
    { key: 'refresh_token', label: t('accounts.compat_import_format_rt_input') },
  ], [t]);
  const selectedFormatLabel = formatOptions.find((option) => option.key === selection)?.label;
  const inputLines = useMemo(
    () => lineInputText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean),
    [lineInputText],
  );
  const isJSONInput = selection === 'json_input';
  const isRTInput = selection === 'refresh_token';
  const isLineInput = isJSONInput || isRTInput;
  const activeFiles = selection && !isLineInput ? files : [];
  const activeInputLines = isLineInput ? inputLines : [];
  const totalInputs = activeFiles.length + activeInputLines.length;
  const totalBytes = useMemo(
    () => {
      if (selection && !isLineInput) {
        return files.reduce((total, file) => total + file.size, 0);
      }
      if (isLineInput) {
        return inputLines.reduce((total, line) => total + new TextEncoder().encode(line).byteLength, 0);
      }
      return 0;
    },
    [files, inputLines, isLineInput, selection],
  );
  const tooManyInputs = totalInputs > MAX_COMPAT_IMPORT_INPUTS;
  const tooLarge = totalBytes > MAX_COMPAT_IMPORT_BYTES;
  const canSubmit = Boolean(selection) && totalInputs > 0 && !tooManyInputs && !tooLarge && !busy;
  const progressPercent = progress && progress.total > 0
    ? Math.round((progress.done / progress.total) * 100)
    : 0;
  const successPercent = progress && progress.total > 0
    ? (progress.success / progress.total) * 100
    : 0;
  const failedPercent = progress && progress.total > 0
    ? (progress.failed / progress.total) * 100
    : 0;

  const handleFileSelection = (event: React.ChangeEvent<HTMLInputElement>) => {
    const selected = Array.from(event.target.files ?? []);
    event.target.value = '';
    if (selected.length === 0) return;
    setReadError('');
    setFiles((current) => mergeFiles(current, selected));
  };

  const handleSubmit = async () => {
    if (!canSubmit || !selection) return;
    setReadError('');
    setProgress(isRTInput ? {
      done: 0,
      failed: 0,
      success: 0,
      total: activeInputLines.length,
    } : null);
    setIsPreparing(true);
    try {
      const fileInputs = await Promise.all(activeFiles.map(async (file) => ({
        name: file.webkitRelativePath || file.name,
        content: await file.text(),
      })));
      const pastedInputs = activeInputLines.map((content, index) => ({
        name: isRTInput
          ? `refresh-token-${String(index + 1).padStart(3, '0')}.txt`
          : `pasted-account-${String(index + 1).padStart(3, '0')}.json`,
        content,
      }));
      const format: CompatImportFormat = selection === 'json_input' ? 'account_json' : selection;
      const imported = await onSubmit(format, [...fileInputs, ...pastedInputs], setProgress);
      if (imported) onClose();
    } catch {
      setReadError(t('accounts.compat_import_read_failed'));
    } finally {
      setIsPreparing(false);
    }
  };

  return (
    <CommonModal
      className="ag-account-page-modal"
      description={t('accounts.compat_import_description')}
      dialogStyle={{
        height: 'min(720px, calc(100dvh - 2rem))',
        maxWidth: '700px',
        width: 'min(100%, calc(100vw - 2rem))',
      }}
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose} isDisabled={busy}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="primary"
            onPress={handleSubmit}
            isDisabled={!canSubmit}
            aria-busy={busy}
          >
            {busy ? <Spinner size="sm" /> : null}
            {t('accounts.compat_import_submit')}
          </Button>
        </div>
      )}
      size="lg"
      state={modalState}
      surface={false}
      title={t('accounts.compat_import_title')}
    >
      <div className="space-y-5">
        <div className="space-y-1.5">
          <Label>{t('accounts.compat_import_format')}</Label>
          <SimpleSelect
            ariaLabel={t('accounts.compat_import_format')}
            fullWidth
            items={formatOptions}
            placeholder={t('accounts.compat_import_format_placeholder')}
            popoverClassName="max-h-80 overflow-y-auto"
            selectedKey={selection}
            selectedLabel={selectedFormatLabel}
            onSelectionChange={(key) => {
              setProgress(null);
              setSelection(key as CompatImportSelection);
            }}
          />
          <p className="text-xs leading-5 text-text-tertiary">
            {t('accounts.compat_import_format_hint')}
          </p>
        </div>

        {selection && !isLineInput ? (
          <section className="space-y-3 rounded-lg border border-border bg-surface p-4">
            <div>
              <h3 className="text-sm font-semibold text-text">{t('accounts.compat_import_files')}</h3>
              <p className="mt-1 text-xs leading-5 text-text-tertiary">
                {t('accounts.compat_import_files_hint')}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" onPress={() => fileInputRef.current?.click()} isDisabled={busy}>
                <Files className="h-4 w-4" />
                {t('accounts.compat_import_select_files')}
              </Button>
              <Button variant="secondary" onPress={() => folderInputRef.current?.click()} isDisabled={busy}>
                <FolderOpen className="h-4 w-4" />
                {t('accounts.compat_import_select_folder')}
              </Button>
              {files.length > 0 ? (
                <Button variant="ghost" onPress={() => setFiles([])} isDisabled={busy}>
                  <Trash2 className="h-4 w-4" />
                  {t('common.clear')}
                </Button>
              ) : null}
            </div>
            {files.length > 0 ? (
              <div className="max-h-32 overflow-y-auto rounded-md border border-border bg-bg-elevated px-3 py-2">
                {files.map((file) => (
                  <div key={fileIdentity(file)} className="flex items-center gap-2 py-1 text-xs text-text-secondary">
                    <FileJson2 className="h-3.5 w-3.5 shrink-0 text-text-tertiary" />
                    <span className="min-w-0 flex-1 truncate">{file.webkitRelativePath || file.name}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </section>
        ) : null}

        {isLineInput ? (
          <section className="space-y-3">
            <HeroTextField fullWidth>
              <Label>
                {isRTInput
                  ? t('accounts.compat_import_rt_lines')
                  : t('accounts.compat_import_json_lines')}
              </Label>
              <TextArea
                value={lineInputText}
                rows={8}
                wrap="off"
                placeholder={isRTInput
                  ? t('accounts.compat_import_rt_placeholder')
                  : t('accounts.compat_import_json_placeholder')}
                disabled={busy}
                onChange={(event) => {
                  setReadError('');
                  setLineInputText(event.target.value);
                }}
              />
            </HeroTextField>
            <p className="text-xs leading-5 text-text-tertiary">
              {isRTInput
                ? t('accounts.compat_import_rt_hint')
                : t('accounts.compat_import_json_hint')}
            </p>
          </section>
        ) : null}

        {isRTInput && progress ? (
          <div className="space-y-2 rounded-lg border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)] px-3 py-2.5">
            <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs">
              <span className="text-[var(--ag-text-secondary)]">
                {t('accounts.compat_import_rt_progress', { done: progress.done, total: progress.total })}
              </span>
              <div className="flex items-center gap-3">
                <span className="inline-flex items-center gap-1 text-success">
                  <Check className="h-3.5 w-3.5" />
                  {t('accounts.compat_import_rt_success_count', { count: progress.success })}
                </span>
                <span className="inline-flex items-center gap-1 text-danger">
                  <X className="h-3.5 w-3.5" />
                  {t('accounts.compat_import_rt_failed_count', { count: progress.failed })}
                </span>
                <span className="font-mono tabular-nums text-[var(--ag-text-secondary)]">
                  {progressPercent}%
                </span>
              </div>
            </div>
            <div className="flex h-1.5 overflow-hidden rounded-full bg-[var(--ag-glass-border)]">
              <div
                className="h-full bg-success transition-[width] duration-300"
                style={{ width: `${successPercent}%` }}
              />
              <div
                className="h-full bg-danger transition-[width] duration-300"
                style={{ width: `${failedPercent}%` }}
              />
            </div>
          </div>
        ) : null}

        {selection ? (
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-text-secondary">
            <span>
              {isRTInput
                ? t('accounts.compat_import_rt_count', { count: inputLines.length })
                : isJSONInput
                  ? t('accounts.compat_import_json_count', { count: inputLines.length })
                : t('accounts.compat_import_file_count', { count: files.length })}
            </span>
            <span>{t('accounts.compat_import_input_limit', {
              count: MAX_COMPAT_IMPORT_INPUTS,
              size: MAX_COMPAT_IMPORT_MIB,
            })}</span>
          </div>
        ) : null}
        {tooManyInputs ? (
          <p className="text-sm text-danger">{t('accounts.compat_import_too_many', { count: MAX_COMPAT_IMPORT_INPUTS })}</p>
        ) : null}
        {tooLarge ? (
          <p className="text-sm text-danger">{t('accounts.compat_import_too_large', { size: MAX_COMPAT_IMPORT_MIB })}</p>
        ) : null}
        {readError ? <p className="text-sm text-danger">{readError}</p> : null}

        <input
          ref={fileInputRef}
          type="file"
          accept="application/json,.json"
          multiple
          className="hidden"
          aria-label={t('accounts.compat_import_select_files')}
          onChange={handleFileSelection}
        />
        <input
          ref={folderInputRef}
          type="file"
          accept="application/json,.json"
          multiple
          className="hidden"
          aria-label={t('accounts.compat_import_select_folder')}
          onChange={handleFileSelection}
        />
      </div>
    </CommonModal>
  );
}
