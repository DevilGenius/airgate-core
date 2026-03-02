import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { settingsApi } from '../../shared/api/settings';
import { useToast } from '../../shared/components/Toast';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Card } from '../../shared/components/Card';
import type { SettingResp, SettingItem } from '../../shared/types';

export default function SettingsPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  // 本地编辑状态：{ key: value }
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);

  // 获取设置列表
  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: () => settingsApi.list(),
  });

  // 当设置数据加载后，初始化编辑状态
  useEffect(() => {
    if (settings) {
      const values: Record<string, string> = {};
      for (const s of settings) {
        values[s.key] = s.value;
      }
      setEditedValues(values);
      setHasChanges(false);
    }
  }, [settings]);

  // 保存设置
  const saveMutation = useMutation({
    mutationFn: (items: SettingItem[]) => settingsApi.update({ settings: items }),
    onSuccess: () => {
      toast('success', '设置已保存');
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      setHasChanges(false);
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新某个设置值
  function handleChange(key: string, value: string) {
    setEditedValues((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  }

  // 提交保存
  function handleSave() {
    const items: SettingItem[] = Object.entries(editedValues).map(([key, value]) => ({
      key,
      value,
    }));
    saveMutation.mutate(items);
  }

  // 按 group 分组
  function groupSettings(list: SettingResp[]): Record<string, SettingResp[]> {
    const groups: Record<string, SettingResp[]> = {};
    for (const s of list) {
      const group = s.group || '通用';
      if (!groups[group]) groups[group] = [];
      groups[group].push(s);
    }
    return groups;
  }

  if (isLoading) {
    return (
      <div className="p-6">
        <PageHeader title="系统设置" />
        <div className="flex items-center justify-center py-12">
          <svg className="animate-spin h-6 w-6 text-indigo-600" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
          <span className="ml-2 text-gray-500">加载中...</span>
        </div>
      </div>
    );
  }

  const groups = groupSettings(settings ?? []);

  return (
    <div className="p-6">
      <PageHeader
        title="系统设置"
        actions={
          <Button onClick={handleSave} loading={saveMutation.isPending} disabled={!hasChanges}>
            保存
          </Button>
        }
      />

      {Object.keys(groups).length === 0 ? (
        <div className="text-center py-12 text-gray-500">暂无设置项</div>
      ) : (
        <div className="space-y-6">
          {Object.entries(groups).map(([group, items]) => (
            <Card key={group} title={group}>
              <div className="space-y-4">
                {items.map((setting) => (
                  <div key={setting.key} className="flex items-center gap-4">
                    <label className="w-48 text-sm font-medium text-gray-700 shrink-0">
                      {setting.key}
                    </label>
                    <Input
                      value={editedValues[setting.key] ?? setting.value}
                      onChange={(e) => handleChange(setting.key, e.target.value)}
                      className="flex-1"
                    />
                  </div>
                ))}
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
