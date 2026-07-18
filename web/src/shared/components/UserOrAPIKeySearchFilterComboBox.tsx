import { apikeysApi } from '../api/apikeys';
import { usersApi } from '../api/users';
import { queryKeys } from '../queryKeys';
import type { APIKeyResp, UserResp } from '../types';
import { formatAPIKeyHint } from '../utils/format';
import { RemoteSearchFilterComboBox } from './RemoteSearchFilterComboBox';

export type UserOrAPIKeySearchKind = 'user' | 'api_key';

export type UserOrAPIKeySearchSelection = {
  id: string;
  kind: UserOrAPIKeySearchKind;
  label: string;
};

type UserOrAPIKeySearchItem = UserOrAPIKeySearchSelection & {
  description?: string;
  textValue: string;
};

interface UserOrAPIKeySearchFilterComboBoxProps {
  ariaLabel: string;
  emptyPrompt: string;
  loadingLabel?: string;
  noDataLabel: string;
  onSelectionChange: (selection: UserOrAPIKeySearchSelection | null) => void;
  placeholder: string;
  selectedKey?: string | null;
  selectedKind?: UserOrAPIKeySearchKind;
  selectedLabel?: string;
}

function encodeItemID(kind: UserOrAPIKeySearchKind, id: string) {
  return `${kind}:${id}`;
}

function decodeItemID(value: string): UserOrAPIKeySearchSelection | null {
  const separator = value.indexOf(':');
  if (separator <= 0 || separator === value.length - 1) return null;
  const kind = value.slice(0, separator);
  if (kind !== 'user' && kind !== 'api_key') return null;
  return {
    id: value.slice(separator + 1),
    kind,
    label: '',
  };
}

function isUserSearch(keyword: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(keyword.trim());
}

function mapUserToSearchItem(user: UserResp): UserOrAPIKeySearchItem {
  const label = user.email || user.username || `#${user.id}`;
  return {
    description: user.username && user.username !== user.email ? user.username : undefined,
    id: String(user.id),
    kind: 'user',
    label,
    textValue: `${user.username || ''} ${user.email}`,
  };
}

function mapAPIKeyToSearchItem(key: APIKeyResp): UserOrAPIKeySearchItem {
  const keyHint = formatAPIKeyHint(key.key_prefix);
  const label = key.name || keyHint || `#${key.id}`;
  return {
    description: [
      `#${key.id}`,
      keyHint,
      key.user_id ? `User #${key.user_id}` : '',
    ].filter(Boolean).join(' · '),
    id: String(key.id),
    kind: 'api_key',
    label,
    textValue: `${key.name || ''} ${keyHint || ''} ${key.id || ''}`,
  };
}

export function UserOrAPIKeySearchFilterComboBox({
  selectedKey,
  selectedKind,
  onSelectionChange,
  ...props
}: UserOrAPIKeySearchFilterComboBoxProps) {
  const encodedSelectedKey = selectedKey && selectedKind
    ? encodeItemID(selectedKind, selectedKey)
    : null;

  return (
    <RemoteSearchFilterComboBox<UserOrAPIKeySearchItem>
      {...props}
      buildQueryKey={(keyword) => (
        isUserSearch(keyword)
          ? queryKeys.users('identity-search', keyword)
          : queryKeys.apikeys('admin-identity-search', keyword)
      )}
      mapItemToOption={(item) => ({
        description: item.description,
        id: encodeItemID(item.kind, item.id),
        label: item.label,
        textValue: item.textValue,
      })}
      queryItems={(keyword, signal) => {
        if (isUserSearch(keyword)) {
          return usersApi.list({ page: 1, page_size: 20, keyword }, { signal })
            .then((data) => (data.list ?? []).map(mapUserToSearchItem));
        }
        const params = { page: 1, page_size: 20, keyword, search_scope: 'api_key' as const };
        return apikeysApi.adminList(params, { signal })
          .then((data) => (data.list ?? []).map(mapAPIKeyToSearchItem));
      }}
      selectedKey={encodedSelectedKey}
      onSelectionChange={(value, label) => {
        if (!value) {
          onSelectionChange(null);
          return;
        }
        const selection = decodeItemID(value);
        if (!selection) {
          onSelectionChange(null);
          return;
        }
        onSelectionChange({ ...selection, label });
      }}
    />
  );
}
