import type { dedup as zh } from '../zh-CN/dedup'

export const dedup = {
  title: 'Duplicates',
  subtitle:
    'Image pairs with near-identical perceptual hashes, found by the background scan and decided by you; {total} pending',
  empty: 'No suspected duplicates',
  emptyHint: 'Pairs show up here once the background scan finds similar images',
  candidate: 'Candidate #{id}',
  foundAt: 'Found {time}',
  distance: 'Distance {distance}',
  distanceHint:
    'Number of differing perceptual-hash bits; smaller is more similar, 0–2 is near identical',
  sideA: 'Left',
  sideB: 'Right',
  keepThis: 'Keep this one',
  refs: 'References: {count}',
  meta: '{mime} · {width}×{height} · {size}',
  noPreview: 'No reference to preview',
  openOriginal: 'Open in new tab',
  selectCandidate: 'Select candidate #{id}',
  dismiss: 'Dismiss',
  dismissed: 'Dismissed, this pair will not be shown again',
  dismissSelected: 'Dismiss {count} selected',
  dismissSelectedTitle: 'Dismiss the {count} selected candidates?',
  dismissSelectedDesc:
    'Dismissed pairs are never shown again; merging requires choosing the image to keep for each pair.',
  batchDismissDone: 'Dismissed {done} pairs',
  batchDismissPartial: 'Dismissed {done} pairs, {failed} failed',
  clearSelection: 'Clear selection',
  mergeTitle: 'Merge into the kept image?',
  mergeDesc:
    'The other image’s {refs} references will point to the kept image and every public link keeps working; the merged file is removed after the grace period. This cannot be undone.',
  merge: 'Merge',
  merged: 'Merged',
} satisfies typeof zh
