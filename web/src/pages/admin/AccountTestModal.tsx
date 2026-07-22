import { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Chip, Label, useOverlayState } from '@heroui/react';
import { Play, RotateCcw, Copy, Check, X } from 'lucide-react';
import { accountsApi } from '../../shared/api/accounts';
import { useClipboard } from '../../shared/hooks/useClipboard';
import { CommonModal } from '../../shared/components/CommonModal';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import {
  filterConnectivityTestModels,
  runAccountConnectivityTest,
} from './accounts/accountTestRunner';
import type { AccountResp, ModelInfo } from '../../shared/types';

type TestStatus = 'idle' | 'connecting' | 'streaming' | 'success' | 'error';

interface OutputLine {
  text: string;
  color: string; // tailwind text color class
}

interface TestTiming {
  firstEventMs: number | null;
  durationMs: number;
}

function formatTimingMs(value: number | null): string {
  if (value === null || !Number.isFinite(value) || value <= 0) return '-';
  return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${value}ms`;
}

interface AccountTestModalProps {
  open: boolean;
  account: AccountResp | null;
  onClose: () => void;
  onTestComplete?: (accountId: number, success: boolean) => void;
}

export function AccountTestModal({
  open,
  account,
  onClose,
  onTestComplete,
}: AccountTestModalProps) {
  const { t } = useTranslation();

  const [models, setModels] = useState<ModelInfo[]>([]);
  const [selectedModel, setSelectedModel] = useState('');
  const [loadingModels, setLoadingModels] = useState(false);

  const [status, setStatus] = useState<TestStatus>('idle');
  const [outputLines, setOutputLines] = useState<OutputLine[]>([]);
  const [streamingContent, setStreamingContent] = useState('');
  const [errorMessage, setErrorMessage] = useState('');
  const [timing, setTiming] = useState<TestTiming | null>(null);
  const [copied, setCopied] = useState(false);

  const terminalRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const streamingRef = useRef('');
  const accountId = account?.id;

  // 加载模型列表
  useEffect(() => {
    if (!open || !accountId) return;
    let active = true;
    setModels([]);
    setSelectedModel('');
    setLoadingModels(true);
    accountsApi.models(accountId)
      .then((list) => {
        if (!active) return;
        // 过滤掉生图专用模型：测试流程发的是 chat 格式 {messages:[{role:user,content:"hi"}]}，
        // ChatGPT OAuth 对 gpt-image-* 系列的 chat 调用会直接报 "not supported"。
        // 账号能跑普通 chat 就一定能跑图（图像走派生的 image_generation tool 通道），
        // 不必在测试里单独验证生图。
        const items = filterConnectivityTestModels(list);
        setModels(items);
        if (items.length > 0) setSelectedModel(items[0]!.id);
      })
      .catch(() => {
        if (active) setModels([]);
      })
      .finally(() => {
        if (active) setLoadingModels(false);
      });
    return () => {
      active = false;
    };
  }, [open, accountId]);

  // 关闭弹窗或切换账号时，重置为完整的单账号测试初始态。
  useEffect(() => {
    abortRef.current?.abort();
    streamingRef.current = '';
    setStatus('idle');
    setOutputLines([]);
    setStreamingContent('');
    setErrorMessage('');
    setTiming(null);
    setSelectedModel('');
    setModels([]);
    setLoadingModels(Boolean(open && accountId));
    setCopied(false);
  }, [open, accountId]);

  const scrollToBottom = useCallback(() => {
    requestAnimationFrame(() => {
      if (terminalRef.current) {
        terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
      }
    });
  }, []);

  const addLine = useCallback((text: string, color: string) => {
    setOutputLines((prev) => [...prev, { text, color }]);
    scrollToBottom();
  }, [scrollToBottom]);

  const startTest = useCallback(async () => {
    if (!account) return;

    // 重置
    setOutputLines([]);
    setStreamingContent('');
    streamingRef.current = '';
    setErrorMessage('');
    setTiming(null);
    setStatus('connecting');

    addLine(t('accounts.test_connecting'), 'text-yellow-400');

    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const result = await runAccountConnectivityTest({
        accountId: account.id,
        fallbackError: t('accounts.test_error'),
        modelId: selectedModel,
        signal: controller.signal,
        handlers: {
          onStart: (model) => {
            addLine(t('accounts.test_connected'), 'text-green-400');
            addLine(t('accounts.test_model_used', { model }), 'text-cyan-400');
            addLine(t('accounts.test_sending'), 'text-gray-400');
            addLine(t('accounts.test_response'), 'text-yellow-400');
            setStatus('streaming');
          },
          onTextDelta: (text) => {
            streamingRef.current += text;
            setStreamingContent(streamingRef.current);
            scrollToBottom();
          },
          onRawError: (message) => addLine(message, 'text-red-400'),
        },
      });

      if (streamingRef.current) {
        addLine(streamingRef.current, 'text-green-300');
        streamingRef.current = '';
        setStreamingContent('');
      }
      if (result.success) {
        setTiming({
          firstEventMs: result.firstEventMs ?? null,
          durationMs: result.durationMs ?? 0,
        });
        setStatus('success');
      } else {
        setTiming(null);
        setStatus('error');
        setErrorMessage(result.error || t('accounts.test_error'));
      }
      onTestComplete?.(account.id, result.success);
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      setTiming(null);
      setStatus('error');
      const msg = (err as Error).message;
      setErrorMessage(msg);
      addLine(msg, 'text-red-400');
      onTestComplete?.(account.id, false);
    }
  }, [account, selectedModel, addLine, onTestComplete, scrollToBottom, t]);

  const handleClose = () => {
    abortRef.current?.abort();
    onClose();
  };

  const clipboardCopy = useClipboard();
  const handleCopy = async () => {
    const text = outputLines.map((l) => l.text).join('\n') + (streamingContent ? '\n' + streamingContent : '');
    await clipboardCopy(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });

  if (!account) return null;

  const canStart = status !== 'connecting' && status !== 'streaming' && !!selectedModel;
  const modelOptions = loadingModels
    ? [{ id: '', label: t('common.loading') }]
    : models.map((m) => ({ id: m.id, label: m.name || m.id }));
  const selectedModelLabel = modelOptions.find((item) => item.id === selectedModel)?.label ?? '';
  const isRunning = status === 'connecting' || status === 'streaming';

  return (
    <CommonModal
      className="ag-account-page-modal ag-account-test-modal"
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={handleClose}>
            {t('common.close')}
          </Button>
          <Button
            variant={status === 'error' ? 'danger' : 'primary'}
            onPress={startTest}
            isDisabled={!canStart}
            aria-busy={isRunning}
          >
            {status === 'idle' || status === 'connecting' || status === 'streaming'
              ? <Play className="w-3.5 h-3.5" />
              : <RotateCcw className="w-3.5 h-3.5" />
            }
            {status === 'success' || status === 'error'
              ? t('accounts.retry')
              : t('accounts.start_test')
            }
          </Button>
        </div>
      )}
      icon={<Play className="size-5" />}
      size="md"
      state={modalState}
      title={t('accounts.test_modal_title')}
    >
              <div className="space-y-4">
                {/* 账号信息卡片 */}
                <div className="flex items-center gap-3 p-3 rounded-lg bg-[var(--ag-bg-surface)] border border-[var(--ag-glass-border)]">
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-sm text-[var(--ag-text)] truncate">
                      {account.name}
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                      <Chip color="default" size="sm" variant="soft">{account.platform.toUpperCase()}</Chip>
                      {account.type && <Chip color="accent" size="sm" variant="soft">{account.type}</Chip>}
                    </div>
                  </div>
                </div>

                {/* 模型选择 */}
                <div className="space-y-1.5">
                  <Label>{t('accounts.select_model')}</Label>
                  <SimpleSelect
                    ariaLabel={t('accounts.select_model')}
                    className="ag-account-test-model-select"
                    fullWidth
                    items={modelOptions.map((item) => ({ key: item.id, label: item.label }))}
                    selectedKey={selectedModel}
                    selectedLabel={selectedModelLabel}
                    onSelectionChange={setSelectedModel}
                    isDisabled={isRunning}
                  />
                </div>

                {/* 终端输出区域 */}
                <div className="relative group">
                  <div
                    ref={terminalRef}
                    className="bg-gray-900 rounded-lg border border-gray-700 p-4 font-mono text-xs leading-relaxed overflow-y-auto"
                    style={{ minHeight: 120, maxHeight: 240 }}
                  >
                    {status === 'idle' && outputLines.length === 0 ? (
                      <span className="text-gray-500">{t('accounts.test_ready')}</span>
                    ) : (
                      <>
                        {outputLines.map((line, i) => (
                          <div key={i} className={line.color}>{line.text}</div>
                        ))}
                        {streamingContent && (
                          <span className="text-green-400">
                            {streamingContent}
                            <span className="animate-pulse">_</span>
                          </span>
                        )}
                        {status === 'success' && (
                          <div className="mt-1">
                            <div className="text-green-400">
                              <Check className="w-3.5 h-3.5 inline mr-1" />
                              {t('accounts.test_done')}
                            </div>
                            {timing && (
                              <div className="mt-0.5 text-gray-400">
                                {t('accounts.test_timing', {
                                  firstEvent: formatTimingMs(timing.firstEventMs),
                                  duration: formatTimingMs(timing.durationMs),
                                })}
                              </div>
                            )}
                          </div>
                        )}
                        {status === 'error' && (
                          <div className="text-red-400 mt-1">
                            <X className="w-3.5 h-3.5 inline mr-1" />
                            {errorMessage || t('accounts.test_error')}
                          </div>
                        )}
                      </>
                    )}
                  </div>

                  {/* 复制按钮 */}
                  {outputLines.length > 0 && (
                    <Button
                      aria-label={t('common.copy')}
                      className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity"
                      isIconOnly
                      size="sm"
                      variant="secondary"
                      onPress={handleCopy}
                    >
                      {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                    </Button>
                  )}
                </div>
              </div>
    </CommonModal>
  );
}
