import { useQuery } from '@tanstack/react-query';
import { pluginsApi } from '../api/plugins';

/**
 * 从已安装的 gateway 插件中动态获取可用平台列表。
 * 没有安装任何插件时返回空数组。
 */
export function usePlatforms() {
  const { data, isLoading } = useQuery({
    queryKey: ['installed-platforms'],
    queryFn: async () => {
      const resp = await pluginsApi.list({ page: 1, page_size: 100 });
      // 所有运行中的插件平台
      const platforms = resp.list
        .map((p) => p.platform)
        .filter(Boolean);
      // 去重
      return [...new Set(platforms)];
    },
    staleTime: 60_000, // 平台列表不会频繁变化，缓存 1 分钟
  });

  return { platforms: data ?? [], isLoading };
}
