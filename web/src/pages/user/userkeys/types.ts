export interface KeyForm {
  name: string;
  group_id: string;
  quota_usd: string;
  /** 销售倍率（reseller markup）。"1" 表示不加价 */
  sell_rate: string;
  /** API Key 级并发上限。空字符串或 "0" 表示不限制 */
  max_concurrency: string;
  expires_at: string;
}

export const emptyForm: KeyForm = {
  name: '',
  group_id: '',
  quota_usd: '',
  sell_rate: '1',
  max_concurrency: '',
  expires_at: '',
};
