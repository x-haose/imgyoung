import type { cleanup as zh } from '../zh-CN/cleanup'

export const cleanup = {
  title: 'Cleanup',
  subtitleEnabled:
    'Images with no public access for over {days} days; {total} waiting. Nothing is deleted automatically; you decide whether to keep or delete each',
  subtitleDisabled:
    'Images with no public access for a long time; {total} waiting. Nothing is deleted automatically; you decide whether to keep or delete each',
  disabledNotice:
    'The cold image threshold (cold_cleanup_days) is 0, so no new images will appear here.',
  goSettings: 'Open settings',
  empty: 'No cold images waiting',
  emptyHint:
    'Images nobody has viewed past the cold image threshold appear here',
  preview: 'Preview',
  image: 'Image',
  owner: 'Owner',
  lastAccess: 'Last access',
  neverAccessed: 'Never accessed',
  uploadedAt: 'Uploaded',
  decision: 'Decision',
  meta: '{mime} · {width}×{height} · {size}',
  selectAllPage: 'Select this page',
  selectRow: 'Select {name}',
  selected: '{count} selected',
  clearSelection: 'Clear selection',
  keep: 'Keep',
  keepTitle: 'Keep this cold image?',
  keepDesc:
    'The image gets a cleanup exemption and will not be marked for cleanup again; you can remove the exemption in the image details.',
  kept: 'Kept and marked as cleanup exempt',
  keepSelected: 'Keep {count} selected',
  keepSelectedTitle: 'Keep the {count} selected images?',
  batchKeepDone: 'Kept {done} images',
  batchKeepPartial: 'Kept {done} images, {failed} failed',
  delete: 'Delete',
  deleteTitle: 'Delete this cold image?',
  deleteDesc:
    'Links stop working immediately and this cannot be undone; physical files with no other references are removed in about {minutes} minutes.',
  deleteDescNoGrace:
    'Links stop working immediately and this cannot be undone; physical files with no other references are removed in the next cleanup sweep.',
  deleted: 'Deleted',
  deleteSelected: 'Delete {count} selected',
  deleteSelectedTitle: 'Delete the {count} selected images?',
  batchDeleteDone: 'Deleted {count} images',
  batchDeletePartial: 'Deleted {deleted} images, {failed} failed: {error}',
} satisfies typeof zh
