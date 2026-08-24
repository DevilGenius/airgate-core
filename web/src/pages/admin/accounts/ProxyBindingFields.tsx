import { Input, Label, TextField as HeroTextField } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import type { ProxyResp } from '../../../shared/types';
import { formatProxySlot, parseProxySlotNumber, proxyEndpointLabel } from '../../../shared/utils/proxy';

export type ProxySlotAssignment = 'random' | 'custom';

export function resolveProxyBinding(proxies: ProxyResp[], proxyId: number | null | undefined, slotInput: string) {
  const proxy = proxies.find((item) => item.id === proxyId);
  if (proxy?.mode !== 'group') {
    return { proxy, valid: true, assignment: undefined, slot: undefined } as const;
  }
  const normalized = slotInput.trim().toLowerCase();
  if (normalized === 'random') {
    return { proxy, valid: true, assignment: 'random' as const, slot: undefined };
  }
  const slot = parseProxySlotNumber(slotInput);
  const valid = slot != null
    && slot >= (proxy.slot_start ?? 0)
    && slot <= (proxy.slot_end ?? 0);
  return { proxy, valid, assignment: 'custom' as const, slot: valid ? slot : undefined };
}

export function ProxyBindingFields({
  disabled = false,
  emptyLabel,
  onProxyChange,
  onSlotChange,
  proxies,
  proxyId,
  slotInput,
}: {
  disabled?: boolean;
  emptyLabel: string;
  onProxyChange: (proxyId: number | null, proxy: ProxyResp | undefined) => void;
  onSlotChange: (value: string) => void;
  proxies: ProxyResp[];
  proxyId: number | null | undefined;
  slotInput: string;
}) {
  const { t } = useTranslation();
  const binding = resolveProxyBinding(proxies, proxyId, slotInput);
  const options = [
    { id: '', label: emptyLabel },
    ...proxies.map((proxy) => ({ id: String(proxy.id), label: proxyEndpointLabel(proxy) })),
  ];
  const selectedLabel = options.find((item) => item.id === (proxyId == null ? '' : String(proxyId)))?.label ?? emptyLabel;
  const slotMapping = binding.proxy?.mode === 'group'
    ? binding.assignment === 'random'
      ? 'random'
      : binding.slot == null
        ? ''
        : `${binding.slot} → ${formatProxySlot(binding.slot, '')}`
    : '';

  return (
    <div className="ag-account-proxy-binding-row">
      <div className="min-w-0 space-y-1.5">
        <Label>{t('accounts.proxy')}</Label>
        <SimpleSelect
          ariaLabel={t('accounts.proxy')}
          fullWidth
          isDisabled={disabled}
          items={options.map((item) => ({ key: item.id, label: item.label }))}
          selectedKey={proxyId == null ? '' : String(proxyId)}
          selectedLabel={selectedLabel}
          onSelectionChange={(key) => {
            const nextID = key ? Number(key) : null;
            onProxyChange(nextID, proxies.find((proxy) => proxy.id === nextID));
          }}
        />
      </div>
      {binding.proxy?.mode === 'group' ? (
        <HeroTextField fullWidth isDisabled={disabled} isInvalid={!binding.valid}>
          <Label className="block max-w-full truncate whitespace-nowrap">
            {slotMapping ? `Slot (${slotMapping})` : 'Slot'}
          </Label>
          <Input
            maxLength={6}
            placeholder="random"
            value={slotInput}
            onChange={(event) => {
              const next = event.target.value.toLowerCase();
              if (!/^\d*$/.test(next) && !'random'.startsWith(next)) return;
              onSlotChange(next);
            }}
          />
        </HeroTextField>
      ) : (
        <HeroTextField
          aria-hidden="true"
          className="invisible pointer-events-none"
          fullWidth
          isDisabled
        >
          <Label className="block max-w-full truncate whitespace-nowrap">Slot</Label>
          <Input tabIndex={-1} value="" readOnly />
        </HeroTextField>
      )}
    </div>
  );
}
