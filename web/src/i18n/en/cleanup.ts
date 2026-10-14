import type { cleanup as zh } from '../zh-CN/cleanup'

export const cleanup = {
  title: 'Cleanup',
  subtitleEnabled:
    'Images with no public access for over {days} days land here; {total} waiting. Nothing is deleted automatically',
  subtitleDisabled:
    'Cold images with no public access past the threshold; {total} waiting. Nothing is deleted automatically',
  disabledNotice:
    'Cold-image tagging is off (cold_cleanup_days is 0), so nothing new will appear here.',
  goSettings: 'Open settings',
  empty: 'No cold images waiting',
  emptyHint:
    'Images nobody has viewed past the threshold wait here for your decision',
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
    'Exempt images are no longer tagged as cold; you can undo this from the image details.',
  kept: 'Kept, no longer tagged as cold',
  keepSelected: 'Keep {count} selected',
  keepSelectedTitle: 'Keep the {count} selected images?',
  batchKeepDone: 'Kept {done} images',
  batchKeepPartial: 'Kept {done} images, {failed} failed',
  delete: 'Delete',
  deleteTitle: 'Delete this cold image?',
  deleteDesc:
    'Public links stop working immediately and this cannot be undone; files with no other references are removed in about {minutes} minutes.',
  deleteDescNoGrace:
    'Public links stop working immediately and this cannot be undone; files with no other references are removed shortly.',
  deleted: 'Deleted',
  deleteSelected: 'Delete {count} selected',
  deleteSelectedTitle: 'Delete the {count} selected images?',
  batchDeleteDone: 'Deleted {count} images',
  batchDeletePartial: 'Deleted {deleted} images, {failed} failed: {error}',
} satisfies typeof zh
