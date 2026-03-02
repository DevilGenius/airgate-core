interface BadgeProps {
  children: string;
  variant?: 'default' | 'success' | 'warning' | 'danger' | 'info';
}

const variantStyles = {
  default: 'bg-gray-100 text-gray-700',
  success: 'bg-green-100 text-green-700',
  warning: 'bg-yellow-100 text-yellow-700',
  danger: 'bg-red-100 text-red-700',
  info: 'bg-blue-100 text-blue-700',
};

export function Badge({ children, variant = 'default' }: BadgeProps) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${variantStyles[variant]}`}>
      {children}
    </span>
  );
}

// 状态标签的快捷映射
const statusMap: Record<string, { variant: BadgeProps['variant']; label: string }> = {
  active: { variant: 'success', label: '活跃' },
  enabled: { variant: 'success', label: '已启用' },
  disabled: { variant: 'default', label: '已禁用' },
  error: { variant: 'danger', label: '错误' },
  expired: { variant: 'warning', label: '已过期' },
  suspended: { variant: 'warning', label: '已暂停' },
  pending: { variant: 'info', label: '待处理' },
  paid: { variant: 'success', label: '已支付' },
  failed: { variant: 'danger', label: '失败' },
  installed: { variant: 'info', label: '已安装' },
};

export function StatusBadge({ status }: { status: string }) {
  const config = statusMap[status] || { variant: 'default' as const, label: status };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}
