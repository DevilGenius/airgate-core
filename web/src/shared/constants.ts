/** 默认分页大小 */
export const DEFAULT_PAGE_SIZE = 20;

/** 分页大小选项 */
export const PAGE_SIZE_OPTIONS = [20, 50, 100] as const;

/** 全量拉取参数（用于下拉选择等场景） */
export const FETCH_ALL_PARAMS = { page: 1, page_size: 100 } as const;

/** 头像颜色池（引用 SDK 装饰色） */
export { decorativePalette as AVATAR_COLORS } from '@airgate/theme';
