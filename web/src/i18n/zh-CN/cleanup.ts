/** 待清理页:冷图列表与保留 / 删除裁决。 */
export const cleanup = {
  title: '待清理',
  subtitleEnabled:
    '超过 {days} 天无公开访问的图片进入此列表，共 {total} 张；不会自动删除，由你裁决',
  subtitleDisabled:
    '超期无公开访问的冷图，共 {total} 张；不会自动删除，由你裁决',
  disabledNotice:
    '冷图打标未启用（cold_cleanup_days 为 0），此列表不会有新条目。',
  goSettings: '去设置',
  empty: '暂无待清理的冷图',
  emptyHint: '超过阈值仍无人访问的图片会在这里等你裁决',
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
  keepDesc: '豁免后不再参与冷图打标，可在图片详情中取消。',
  kept: '已保留，不再参与冷图打标',
  keepSelected: '保留选中的 {count} 张',
  keepSelectedTitle: '保留选中的 {count} 张图片？',
  batchKeepDone: '已保留 {done} 张',
  batchKeepPartial: '已保留 {done} 张，{failed} 张失败',
  delete: '删除',
  deleteTitle: '删除这张冷图？',
  deleteDesc:
    '删除后外链立即失效，该操作不可恢复；无其他引用的物理文件将在约 {minutes} 分钟后清理。',
  deleteDescNoGrace:
    '删除后外链立即失效，该操作不可恢复；无其他引用的物理文件将很快清理。',
  deleted: '已删除',
  deleteSelected: '删除选中的 {count} 张',
  deleteSelectedTitle: '删除选中的 {count} 张图片？',
  batchDeleteDone: '已删除 {count} 张',
  batchDeletePartial: '已删除 {deleted} 张，{failed} 张失败：{error}',
}
