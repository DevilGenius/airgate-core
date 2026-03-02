interface EmptyStateProps {
  title?: string;
  description?: string;
  icon?: string;
}

export function EmptyState({
  title = '暂无数据',
  description = '当前没有可显示的内容',
  icon = '📭',
}: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <span className="text-4xl mb-3">{icon}</span>
      <p className="text-gray-900 font-medium">{title}</p>
      <p className="text-sm text-gray-500 mt-1">{description}</p>
    </div>
  );
}
