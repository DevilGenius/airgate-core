import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';
import { proxiesApi } from '../../shared/api/proxies';
import { useToast } from '../../shared/ui';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { usePagination } from '../../shared/hooks/usePagination';
import { AlertDialog, Button, Chip, EmptyState, Form, Input, Label, Modal, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { StatusChip } from '../../shared/ui';
import { Plus, Pencil, Trash2, Zap, RefreshCw } from 'lucide-react';
import type { ProxyResp, CreateProxyReq, UpdateProxyReq } from '../../shared/types';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { TablePage } from '../../shared/components/TablePage';
import { formatProxySlot } from '../../shared/utils/proxy';

// 代理表单数据
interface ProxyForm {
  name: string;
  mode: 'single' | 'group';
  protocol: 'http' | 'socks5';
  address: string;
  port: string;
  username: string;
  password: string;
  slotStart: string;
  slotEnd: string;
}

const emptyForm: ProxyForm = {
  name: '',
  mode: 'single',
  protocol: 'http',
  address: '',
  port: '',
  username: '',
  password: '',
  slotStart: '0000',
  slotEnd: 'ffff',
};

function parseSlot(value: string) {
  const normalized = value.trim();
  if (!/^[0-9a-f]{1,4}$/i.test(normalized)) return null;
  return Number.parseInt(normalized, 16);
}

export default function ProxiesPage() {
  const { t } = useTranslation();
  const { toast, updateToast } = useToast();

  const { page, setPage, pageSize, setPageSize } = usePagination(20, 'admin.proxies');
  const [modalOpen, setModalOpen] = useState(false);
  const [editingProxy, setEditingProxy] = useState<ProxyResp | null>(null);
  const [form, setForm] = useState<ProxyForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<ProxyResp | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);

  // 查询代理列表
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: queryKeys.proxies(page, pageSize),
    queryFn: () => proxiesApi.list({ page, page_size: pageSize }),
    placeholderData: keepPreviousData,
  });

  // 创建代理
  const createMutation = useCrudMutation({
    mutationFn: (data: CreateProxyReq) => proxiesApi.create(data),
    successMessage: t('proxies.create_success'),
    queryKey: queryKeys.proxies(),
    onSuccess: () => closeModal(),
  });

  // 更新代理
  const updateMutation = useCrudMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateProxyReq }) =>
      proxiesApi.update(id, data),
    successMessage: t('proxies.update_success'),
    queryKey: queryKeys.proxies(),
    onSuccess: () => closeModal(),
  });

  // 删除代理
  const deleteMutation = useCrudMutation({
    mutationFn: (id: number) => proxiesApi.delete(id),
    successMessage: t('proxies.delete_success'),
    queryKey: queryKeys.proxies(),
    onSuccess: () => setDeleteTarget(null),
  });

  // 出口 IP 查询独立执行，不阻塞连通性测试。
  const ipLookupMutation = useMutation({
    mutationFn: ({ id }: { id: number; latencyMs: number; toastId: string }) => proxiesApi.lookupIP(id),
    onSuccess: (result, { latencyMs, toastId }) => {
      const parts = [`${latencyMs}ms`];
      if (!result.success || !result.ip_address) {
        parts.push(t('proxies.ip_lookup_failed'));
        updateToast(toastId, t('proxies.test_success', { detail: parts.join('  |  ') }));
        return;
      }
      const location = [result.country, result.city].filter(Boolean).join(' · ');
      parts.push(result.ip_address);
      if (location) parts.push(location);
      updateToast(toastId, t('proxies.test_success', { detail: parts.join('  |  ') }));
    },
    onError: (_error, { latencyMs, toastId }) => {
      const detail = [`${latencyMs}ms`, t('proxies.ip_lookup_failed')].join('  |  ');
      updateToast(toastId, t('proxies.test_success', { detail }));
    },
  });

  // 测试连通性
  const testMutation = useMutation({
    mutationFn: (id: number) => proxiesApi.test(id),
    onSuccess: (result, id) => {
      if (result.success) {
        const location = [result.country, result.city].filter(Boolean).join(' · ');
        const parts = [`${result.latency_ms}ms`];
        if (result.ip_address) parts.push(result.ip_address);
        if (location) parts.push(location);
        if (!result.ip_address) {
          parts.push(t('proxies.ip_lookup_pending'));
          const toastId = toast(
            'success',
            t('proxies.test_success', { detail: parts.join('  |  ') }),
            undefined,
            { timeout: 0 },
          );
          ipLookupMutation.mutate({ id, latencyMs: result.latency_ms, toastId });
        } else {
          toast('success', t('proxies.test_success', { detail: parts.join('  |  ') }));
        }
      } else {
        toast('error', t('proxies.test_failed', { error: result.error_msg || '' }));
      }
      setTestingId(null);
    },
    onError: (err: Error) => {
      toast('error', err.message);
      setTestingId(null);
    },
  });

  // 打开创建弹窗
  function openCreate() {
    setEditingProxy(null);
    setForm(emptyForm);
    setModalOpen(true);
  }

  // 打开编辑弹窗
  function openEdit(proxy: ProxyResp) {
    setEditingProxy(proxy);
    setForm({
      name: proxy.name,
      mode: proxy.mode ?? 'single',
      protocol: proxy.protocol,
      address: proxy.address,
      port: String(proxy.port),
      username: proxy.mode === 'group' ? '' : (proxy.username || ''),
      password: '',
      slotStart: formatProxySlot(proxy.slot_start, '0000'),
      slotEnd: formatProxySlot(proxy.slot_end, 'ffff'),
    });
    setModalOpen(true);
  }

  // 关闭弹窗
  function closeModal() {
    setModalOpen(false);
    setEditingProxy(null);
    setForm(emptyForm);
  }

  // 提交表单
  function handleSubmit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!form.name || !form.address || !form.port) {
      toast('error', t('common.fill_required'));
      return;
    }

    const mode = form.mode;
    let slotStart = 0;
    let slotEnd = 0;
    if (mode === 'group') {
      const parsedStart = parseSlot(form.slotStart);
      const parsedEnd = parseSlot(form.slotEnd);
      if (parsedStart == null || parsedEnd == null || parsedStart > parsedEnd) {
        toast('error', t('proxies.slot_range_invalid'));
        return;
      }
      slotStart = parsedStart;
      slotEnd = parsedEnd;
    }

    const payload = {
      name: form.name,
      mode,
      protocol: form.protocol,
      address: form.address,
      port: Number(form.port),
      username: mode === 'single' ? (form.username || undefined) : undefined,
      password: form.password || undefined,
      slot_start: slotStart,
      slot_end: slotEnd,
    };

    if (editingProxy) {
      updateMutation.mutate({ id: editingProxy.id, data: payload });
    } else {
      createMutation.mutate(payload as CreateProxyReq);
    }
  }

  // 测试连通性
  function handleTest(id: number) {
    setTestingId(id);
    testMutation.mutate(id);
  }

  const saving = createMutation.isPending || updateMutation.isPending;
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const protocolOptions = [
    { id: 'http', label: 'HTTP' },
    { id: 'socks5', label: 'SOCKS5' },
  ];
  const selectedProtocolLabel = protocolOptions.find((item) => item.id === form.protocol)?.label ?? 'HTTP';

  const proxyUsernameDisplay = (proxy: ProxyResp) => {
    if (proxy.mode !== 'group') return proxy.username || '-';
    const range = `${formatProxySlot(proxy.slot_start, '0000')}-${formatProxySlot(proxy.slot_end, 'ffff')}`;
    const capacity = Math.max(0, (proxy.slot_end ?? 0) - (proxy.slot_start ?? 0) + 1);
    return `${range} · ${proxy.assigned_slots ?? 0}/${capacity}`;
  };
  const proxyDialogState = useOverlayState({
    isOpen: modalOpen,
    onOpenChange: (open) => {
      if (!open) closeModal();
    },
  });

  return (
    <TablePage
      actions={(
        <>
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            className="ag-page-toolbar-button"
            size="md"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
          <Button className="ag-page-toolbar-button" variant="primary" onPress={openCreate}>
            <Plus className="w-4 h-4" />
            {t('proxies.create')}
          </Button>
        </>
      )}
      footer={(
        <TablePaginationFooter
          page={page}
          pageSize={pageSize}
          setPage={setPage}
          setPageSize={setPageSize}
          total={total}
          totalPages={totalPages}
        />
      )}
      isFetching={isFetching && !isLoading}
    >

      <CommonTable
        ariaLabel={t('proxies.title', 'Proxies')}
        className="ag-proxies-table"
        contentClassName="ag-proxies-table-content"
        minWidth={944}
      >
            <CommonTable.Header>
              <CommonTable.Column id="id" style={{ width: 64 }}>
                {t('common.id')}
              </CommonTable.Column>
              <CommonTable.Column id="name" style={{ width: 152 }}>{t('common.name')}</CommonTable.Column>
              <CommonTable.Column id="protocol" style={{ width: 96 }}>{t('proxies.protocol')}</CommonTable.Column>
              <CommonTable.Column id="endpoint" style={{ width: 224 }}>{t('proxies.address')}</CommonTable.Column>
              <CommonTable.Column id="username" style={{ width: 144 }}>{t('proxies.username')}</CommonTable.Column>
              <CommonTable.Column id="status" style={{ width: 96 }}>{t('common.status')}</CommonTable.Column>
              <CommonTable.Column id="actions" style={{ width: 168 }}>{t('common.actions')}</CommonTable.Column>
            </CommonTable.Header>
            <CommonTable.Body>
              {isLoading ? (
                <TableLoadingRow colSpan={7} />
              ) : rows.length === 0 ? (
                <CommonTable.Row id="empty">
                  <CommonTable.Cell colSpan={7}>
                    <EmptyState>
                      <div className="text-sm text-default-500">{t('common.no_data')}</div>
                    </EmptyState>
                  </CommonTable.Cell>
                </CommonTable.Row>
              ) : (
                rows.map((row) => (
                  <CommonTable.Row id={String(row.id)} key={row.id}>
                    <CommonTable.Cell>
                      <span className="font-mono text-text-tertiary">{row.id}</span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="block max-w-full truncate text-text" title={row.name}>
                        {row.name}
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <Chip color={row.protocol === 'http' ? 'accent' : 'warning'} size="sm" variant="soft">
                        {row.protocol.toUpperCase()}
                      </Chip>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="block max-w-full truncate font-mono" title={`${row.address}:${row.port}`}>
                        {row.address}:{row.port}
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span
                        className="block max-w-full truncate text-text-secondary"
                        title={proxyUsernameDisplay(row)}
                      >
                        {proxyUsernameDisplay(row)}
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <StatusChip status={row.status} />
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="ag-table-row-actions flex justify-center gap-1">
                        <Button
                          size="sm"
                          variant="secondary"
                          onPress={() => openEdit(row)}
                        >
                          <Pencil className="w-3.5 h-3.5" />
                          {t('common.edit')}
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          isDisabled={testingId === row.id}
                          onPress={() => handleTest(row.id)}
                        >
                          {testingId === row.id ? <Spinner size="sm" /> : <Zap className="w-3.5 h-3.5" />}
                          {t('common.test')}
                        </Button>
                        <Button
                          isIconOnly
                          aria-label={t('common.delete')}
                          size="sm"
                          variant="danger-soft"
                          className="text-danger"
                          onPress={() => setDeleteTarget(row)}
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </Button>
                      </div>
                    </CommonTable.Cell>
                  </CommonTable.Row>
                ))
              )}
            </CommonTable.Body>
      </CommonTable>

      {/* 创建/编辑弹窗 */}
      <Modal state={proxyDialogState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog className="ag-elevation-modal">
              <Modal.Header>
                <Modal.Heading>{editingProxy ? t('proxies.edit') : t('proxies.create')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <Form id="proxy-form" className="space-y-4" onSubmit={handleSubmit}>
                  <HeroTextField fullWidth isRequired>
                    <Label>{t('common.name')}</Label>
                    <Input
                      name="name"
                      autoComplete="off"
                      value={form.name}
                      onChange={(e) => setForm({ ...form, name: e.target.value })}
                      placeholder={t('proxies.name_placeholder')}
                      required
                    />
                  </HeroTextField>
                  <div className="space-y-1.5">
                    <Label>{t('proxies.mode')}</Label>
                    <SimpleSelect
                      ariaLabel={t('proxies.mode')}
                      fullWidth
                      items={[
                        { key: 'single', label: t('proxies.mode_single') },
                        { key: 'group', label: t('proxies.mode_group') },
                      ]}
                      selectedKey={form.mode}
                      selectedLabel={form.mode === 'group' ? t('proxies.mode_group') : t('proxies.mode_single')}
                      onSelectionChange={(key) => setForm({ ...form, mode: (key || 'single') as 'single' | 'group' })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label>{t('proxies.protocol')}</Label>
                    <SimpleSelect
                      ariaLabel={t('proxies.protocol')}
                    fullWidth
                      items={protocolOptions.map((item) => ({ key: item.id, label: item.label }))}
                    selectedKey={form.protocol}
                      selectedLabel={selectedProtocolLabel}
                    onSelectionChange={(key) =>
                      setForm({
                        ...form,
                        protocol: (key || 'http') as 'http' | 'socks5',
                      })
                    }
                    />
                  </div>
                  <HeroTextField fullWidth isRequired>
                    <Label>{t('proxies.address')}</Label>
                    <Input
                      value={form.address}
                      onChange={(e) => setForm({ ...form, address: e.target.value })}
                      placeholder={t('proxies.address_placeholder')}
                      required
                    />
                  </HeroTextField>
                  <HeroTextField fullWidth isRequired>
                    <Label>{t('proxies.port')}</Label>
                    <Input
                      type="number"
                      value={form.port}
                      onChange={(e) => setForm({ ...form, port: e.target.value })}
                      placeholder={t('proxies.port_placeholder')}
                      required
                    />
                  </HeroTextField>
                  {form.mode === 'group' ? (
                    <div className="space-y-1.5">
                      <Label>{t('proxies.slot_range')}</Label>
                      <div className="grid grid-cols-2 gap-3">
                        <Input
                          aria-label={t('proxies.slot_start')}
                          maxLength={4}
                          value={form.slotStart}
                          onChange={(e) => setForm({ ...form, slotStart: e.target.value })}
                          placeholder="0000"
                        />
                        <Input
                          aria-label={t('proxies.slot_end')}
                          maxLength={4}
                          value={form.slotEnd}
                          onChange={(e) => setForm({ ...form, slotEnd: e.target.value })}
                          placeholder="ffff"
                        />
                      </div>
                      <p className="text-xs text-text-secondary">{t('proxies.slot_range_hint')}</p>
                    </div>
                  ) : (
                    <HeroTextField fullWidth>
                      <Label>{t('proxies.username')}</Label>
                      <Input
                        name="username"
                        autoComplete="username"
                        value={form.username}
                        onChange={(e) => setForm({ ...form, username: e.target.value })}
                      />
                    </HeroTextField>
                  )}
                  <HeroTextField fullWidth>
                    <Label>{t('proxies.password_label')}</Label>
                    <Input
                      name="password"
                      type="password"
                      value={form.password}
                      onChange={(e) => setForm({ ...form, password: e.target.value })}
                      placeholder={editingProxy ? t('proxies.password_hint') : ''}
                      autoComplete="off"
                    />
                  </HeroTextField>
                </Form>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={closeModal}>
                  {t('common.cancel')}
                </Button>
                <Button variant="primary" isDisabled={saving} onPress={() => handleSubmit()}>
                  {saving ? <Spinner size="sm" /> : null}
                  {editingProxy ? t('common.save') : t('common.create')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      {/* 删除确认 */}
      <AlertDialog
        isOpen={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('proxies.delete_proxy')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('proxies.delete_confirm', { name: deleteTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </TablePage>
  );
}
