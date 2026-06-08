import { apikeysApi } from '../api/apikeys';
import { queryKeys } from '../queryKeys';
import type { APIKeyResp } from '../types';
import { formatAPIKeyHint } from '../utils/format';
import { RemoteSearchFilterComboBox } from './RemoteSearchFilterComboBox';

type APIKeySearchScope = 'admin' | 'user';

interface APIKeySearchFilterComboBoxProps {
  ariaLabel: string;
  emptyPrompt: string;
  loadingLabel?: string;
  noDataLabel: string;
  onSelectionChange: (value: string, label: string) => void;
  placeholder: string;
  scope?: APIKeySearchScope;
  selectedKey?: string | null;
  selectedLabel?: string;
}

function mapAPIKeyToOption(key: APIKeyResp) {
  const keyHint = formatAPIKeyHint(key.key_prefix);
  return {
    id: String(key.id),
    label: key.name || keyHint || `#${key.id}`,
    description: [
      `#${key.id}`,
      keyHint,
      key.user_id ? `User #${key.user_id}` : '',
    ].filter(Boolean).join(' · '),
    textValue: `${key.name || ''} ${keyHint || ''} ${key.id || ''}`,
  };
}

export function APIKeySearchFilterComboBox({
  scope = 'admin',
  ...props
}: APIKeySearchFilterComboBoxProps) {
  return (
    <RemoteSearchFilterComboBox
      {...props}
      buildQueryKey={(keyword) => (
        scope === 'admin'
          ? queryKeys.apikeys('admin-remote-search', 'api_key', keyword)
          : queryKeys.userKeys('remote-search', 'api_key', keyword)
      )}
      mapItemToOption={mapAPIKeyToOption}
      queryItems={(keyword, signal) => {
        const params = { page: 1, page_size: 20, keyword, search_scope: 'api_key' as const };
        return (scope === 'admin'
          ? apikeysApi.adminList(params, { signal })
          : apikeysApi.list(params, { signal })
        ).then((data) => data.list ?? []);
      }}
    />
  );
}
