export interface KeyForm {
  name: string;
  group_id: string;
  quota_usd: string;
  /** 销售倍率（reseller markup）。空字符串或 "0" 表示按平台原价计费 */
  sell_rate: string;
  expires_at: string;
}

export const emptyForm: KeyForm = {
  name: '',
  group_id: '',
  quota_usd: '',
  sell_rate: '',
  expires_at: '',
};
