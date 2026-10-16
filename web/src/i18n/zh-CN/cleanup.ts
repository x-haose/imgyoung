/** 待清理页:冷图列表与保留 / 删除裁决。 */
export const cleanup = {
  title: '待清理',
  subtitleEnabled:
    '超过 {days} 天无公开访问的图片，共 {total} 张；不会自动删除，由你决定保留或删除',
  subtitleDisabled:
    '长期无公开访问的图片，共 {total} 张；不会自动删除，由你决定保留或删除',
  disabledNotice:
    '冷图清理阈值（cold_cleanup_days）为 0，不会再有新的图片进入此列表。',
  goSettings: '去设置',
  empty: '暂无待清理的冷图',
  emptyHint: '超过冷图清理阈值仍无人访问的图片会出现在这里',
  preview: '预览',
  image: '图片',
  owner: '归属',
  lastAccess: '最近访问',
  neverAccessed: '从未访问',
  uploadedAt: '上传时间',
  decision: '裁决',
  meta: '{mime} · {width}×{height} · {size}',
  selectAllPage: '全选本页',
  selectRow: '勾选 {name}',
  selected: '已选 {count} 张',
  clearSelection: '清除选择',
  keep: '保留',
  keepTitle: '保留这张冷图？',
  keepDesc:
    '将为图片设置清理豁免，之后不会再被标为待清理；可在图片详情中取消豁免。',
  kept: '已保留，并设为清理豁免',
  keepSelected: '保留选中的 {count} 张',
  keepSelectedTitle: '保留选中的 {count} 张图片？',
  batchKeepDone: '已保留 {done} 张',
  batchKeepPartial: '已保留 {done} 张，{failed} 张失败',
  delete: '删除',
  deleteTitle: '删除这张冷图？',
  deleteDesc:
    '删除后链接立即失效，且无法恢复；没有其他引用的物理文件约 {minutes} 分钟后清理。',
  deleteDescNoGrace:
    '删除后链接立即失效，且无法恢复；没有其他引用的物理文件将在下一轮清理扫描时删除。',
  deleted: '已删除',
  deleteSelected: '删除选中的 {count} 张',
  deleteSelectedTitle: '删除选中的 {count} 张图片？',
  batchDeleteDone: '已删除 {count} 张',
  batchDeletePartial: '已删除 {deleted} 张，{failed} 张失败：{error}',
}
