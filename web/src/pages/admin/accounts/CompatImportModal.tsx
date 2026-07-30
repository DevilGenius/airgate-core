import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Label,
  Spinner,
  TextArea,
  TextField as HeroTextField,
  useOverlayState,
} from '@heroui/react';
import { FileJson2, Files, FolderOpen, Trash2 } from 'lucide-react';
import { CommonModal } from '../../../shared/components/CommonModal';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';

export type CompatImportFormat =
  | 'sub2api'
  | 'cpa'
  | 'codex'
  | 'cockpit'
  | 'agent_identity'
  | 'account_json';

export type CompatImportInput = {
  name: string;
  content: string;
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
  onSubmit: (format: CompatImportFormat, inputs: CompatImportInput[]) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);
  const [selection, setSelection] = useState<CompatImportSelection>('sub2api');
  const [files, setFiles] = useState<File[]>([]);
  const [jsonLinesText, setJsonLinesText] = useState('');
  const [readError, setReadError] = useState('');
  const [isPreparing, setIsPreparing] = useState(false);
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
  ], [t]);
  const selectedFormatLabel = formatOptions.find((option) => option.key === selection)?.label;
  const jsonLines = useMemo(
    () => jsonLinesText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean),
    [jsonLinesText],
  );
  const isJSONInput = selection === 'json_input';
  const activeFiles = selection && !isJSONInput ? files : [];
  const activeJSONLines = isJSONInput ? jsonLines : [];
  const totalInputs = activeFiles.length + activeJSONLines.length;
  const totalBytes = useMemo(
    () => {
      if (selection && !isJSONInput) {
        return files.reduce((total, file) => total + file.size, 0);
      }
      if (isJSONInput) {
        return jsonLines.reduce((total, line) => total + new TextEncoder().encode(line).byteLength, 0);
      }
      return 0;
    },
    [files, isJSONInput, jsonLines, selection],
  );
  const tooManyInputs = totalInputs > MAX_COMPAT_IMPORT_INPUTS;
  const tooLarge = totalBytes > MAX_COMPAT_IMPORT_BYTES;
  const canSubmit = Boolean(selection) && totalInputs > 0 && !tooManyInputs && !tooLarge && !busy;

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
    setIsPreparing(true);
    try {
      const fileInputs = await Promise.all(activeFiles.map(async (file) => ({
        name: file.webkitRelativePath || file.name,
        content: await file.text(),
      })));
      const pastedInputs = activeJSONLines.map((content, index) => ({
        name: `pasted-account-${String(index + 1).padStart(3, '0')}.json`,
        content,
      }));
      const format: CompatImportFormat = selection === 'json_input' ? 'account_json' : selection;
      const imported = await onSubmit(format, [...fileInputs, ...pastedInputs]);
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
        height: 'min(620px, calc(100dvh - 2rem))',
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
            onSelectionChange={(key) => setSelection(key as CompatImportSelection)}
          />
          <p className="text-xs leading-5 text-text-tertiary">
            {t('accounts.compat_import_format_hint')}
          </p>
        </div>

        {selection && !isJSONInput ? (
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

        {isJSONInput ? (
          <section className="space-y-3 rounded-lg border border-border bg-surface p-4">
            <HeroTextField fullWidth>
              <Label>{t('accounts.compat_import_json_lines')}</Label>
              <TextArea
                value={jsonLinesText}
                rows={8}
                placeholder={t('accounts.compat_import_json_placeholder')}
                disabled={busy}
                onChange={(event) => {
                  setReadError('');
                  setJsonLinesText(event.target.value);
                }}
              />
            </HeroTextField>
            <p className="text-xs leading-5 text-text-tertiary">
              {t('accounts.compat_import_json_hint')}
            </p>
          </section>
        ) : null}

        {selection ? (
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-text-secondary">
            <span>
              {isJSONInput
                ? t('accounts.compat_import_json_count', { count: jsonLines.length })
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
