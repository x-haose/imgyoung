import type { dashboard as zh } from '../zh-CN/dashboard'

export const dashboard = {
  title: 'Dashboard',
  images: 'Images',
  storage: 'Storage used',
  storageHint: 'Original bytes in total, replicas not double counted',
  users: 'Users',
  accesses: 'Accesses',
  accessesHint:
    'Counted per activity flush cycle; repeated hits on one image within a cycle count once',
  trafficEstimate: 'Est. traffic {bytes}',
  pendingCleanup: 'Pending cleanup',
  pendingCleanupHint:
    'Not accessed beyond the cold threshold, waiting for you to keep or delete',
  pendingDedup: 'Possible duplicates',
  pendingDedupHint:
    'Candidate pairs found by the similarity scan, waiting for a side-by-side decision',
  used: 'Space used',
  quota: 'Quota',
  unlimited: 'Unlimited',
  quotaUsage: '{percent}% used',
  channels: 'Storage channels',
  channelsDesc:
    'Health and stored bytes (replica basis) per channel; bars show the share of the site total',
  manageChannels: 'Manage channels',
  unhealthy: 'Unhealthy',
  noChannels: 'No storage channels yet',
  topImages: 'Top images',
  topImagesDesc:
    'Sorted by total accesses; click to open the public link in a new tab',
  accessCount: '{count} hits',
  noTopImages:
    'No access records yet; images show up here once their public links are visited',
  alerts: 'Alerts',
  alertsDesc:
    'Open alerts; each resolves itself once its condition no longer holds',
  alertHistory: 'History',
  allClear: 'All clear',
  allClearDesc: 'Channels healthy, replicas complete, backups fine',
  alertsFailed: 'Failed to load alerts',
  openedAt: 'Opened {time}',
  alertKinds: {
    channel_unhealthy: 'Channel unhealthy',
    replication_failed: 'Replication failed',
    channel_quota_exceeded: 'Channel over quota',
    drain_failed: 'Channel drain failed',
    backup_failed: 'Backup failed',
  },
  backupColocatedTitle: 'The metadata snapshot only lives on this host',
  backupColocatedDesc:
    'Snapshots only go to the healthy channel with the highest priority, which right now is the local channel on this host. Losing the host loses the snapshot too. Give an off-site channel the highest priority to clear this, adding one first if you have none.',
  quickStartTitle: 'Quick start',
  quickStartDesc: 'No images yet. Three steps to get going',
  step1Title: 'Check storage channels',
  step1Desc:
    'The built-in local channel works out of the box; add S3 / R2 and the like on the channels page.',
  step2Title: 'Issue an upload token',
  step2Desc:
    'One named token for PicGo, one for scripts; the secret is shown only once.',
  step3Title: 'Start uploading',
  step3Desc:
    'Upload with curl, PicGo or any S3 client using the token; the guide has copy-paste configs.',
  readGuide: 'Read the guide',
} satisfies typeof zh
