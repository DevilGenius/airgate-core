import { usersApi } from '../api/users';
import { queryKeys } from '../queryKeys';
import type { UserResp } from '../types';
import { RemoteSearchFilterComboBox } from './RemoteSearchFilterComboBox';

interface UserSearchFilterComboBoxProps {
  ariaLabel: string;
  emptyPrompt: string;
  loadingLabel?: string;
  noDataLabel: string;
  onSelectionChange: (value: string, label: string) => void;
  placeholder: string;
  selectedKey?: string | null;
  selectedLabel?: string;
}

function mapUserToOption(user: UserResp) {
  return {
    id: String(user.id),
    label: user.username || user.email,
    description: user.username ? user.email : undefined,
    textValue: `${user.username || ''} ${user.email}`,
  };
}

export function UserSearchFilterComboBox(props: UserSearchFilterComboBoxProps) {
  return (
    <RemoteSearchFilterComboBox
      {...props}
      buildQueryKey={(keyword) => queryKeys.users('remote-search', keyword)}
      mapItemToOption={mapUserToOption}
      queryItems={(keyword, signal) =>
        usersApi.list({ page: 1, page_size: 20, keyword }, { signal }).then((data) => data.list ?? [])}
    />
  );
}
