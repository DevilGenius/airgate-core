import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../../../shared/components/Modal';
import { Button } from '../../../shared/components/Button';
import { Input, Textarea } from '../../../shared/components/Input';
import type { UserResp, AdjustBalanceReq } from '../../../shared/types';

interface BalanceModalProps {
  open: boolean;
  user: UserResp;
  defaultAction: 'add' | 'subtract';
  onClose: () => void;
  onSubmit: (data: AdjustBalanceReq) => void;
  loading: boolean;
}

/**
 * 余额调整弹窗：充值 / 退款共用一个 modal。
 *
 * 布局参考：用户信息头 → 充值金额 → 备注 → 操作后余额预览。
 * 操作后余额根据 action × amount 实时计算，让管理员在点确认前就能
 * 看到最终余额，避免手滑填错金额。
 */
export function BalanceModal({ open, user, defaultAction, onClose, onSubmit, loading }: BalanceModalProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AdjustBalanceReq>({
    action: defaultAction,
    amount: 0,
    remark: t('users.remark_admin_adjust'),
  });

  const isRefund = defaultAction === 'subtract';

  // 操作后余额实时预览
  const afterBalance = useMemo(() => {
    const amount = Number.isFinite(form.amount) ? form.amount : 0;
    const delta = isRefund ? -amount : amount;
    return user.balance + delta;
  }, [user.balance, form.amount, isRefund]);

  // 用户头像的首字母
  const avatarLetter = (user.email || user.username || '?').charAt(0).toUpperCase();

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={isRefund ? t('users.refund') : t('users.topup')}
      width="460px"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>{t('common.cancel')}</Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>{t('common.confirm')}</Button>
        </>
      }
    >
      <div className="space-y-4">
        {/* 用户信息头：头像 + 邮箱 + 当前余额 */}
        <div className="flex items-center gap-3 rounded-lg border border-glass-border bg-bg-elevated px-4 py-3">
          <div
            className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-primary-subtle text-base font-semibold text-primary"
          >
            {avatarLetter}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium text-text">{user.email}</p>
            <p className="mt-0.5 font-mono text-xs text-text-tertiary">
              {t('users.current_balance')}: ${user.balance.toFixed(7)}
            </p>
          </div>
        </div>

        {/* 金额输入 */}
        <div>
          <Input
            label={isRefund ? t('users.refund_amount', '退款金额') : t('users.topup_amount', '充值金额')}
            type="number"
            required
            min="0"
            max={isRefund ? String(user.balance) : undefined}
            step="0.01"
            icon={<span className="text-sm text-text-tertiary">$</span>}
            value={String(form.amount)}
            onChange={(e) => {
              const val = Number(e.target.value);
              setForm({ ...form, amount: isRefund ? Math.min(val, user.balance) : val });
            }}
          />
          {isRefund && (
            <button
              type="button"
              className="mt-1 cursor-pointer text-[11px] text-primary transition-colors hover:text-primary/80"
              onClick={() => setForm({ ...form, amount: user.balance })}
            >
              {t('users.withdraw_all')}
            </button>
          )}
        </div>

        {/* 备注（textarea） */}
        <Textarea
          label={t('users.remark')}
          placeholder={t('users.remark_placeholder')}
          value={form.remark ?? ''}
          onChange={(e) => setForm({ ...form, remark: e.target.value })}
        />

        {/* 操作后余额预览 */}
        <div className="flex items-center justify-between rounded-lg border border-primary/30 bg-primary-subtle px-4 py-3">
          <span className="text-sm text-text-secondary">
            {t('users.balance_after_op', '操作后余额')}:
          </span>
          <span
            className={`font-mono text-lg font-bold ${
              afterBalance < 0 ? 'text-danger' : 'text-text'
            }`}
          >
            ${afterBalance.toFixed(7)}
          </span>
        </div>
      </div>
    </Modal>
  );
}
