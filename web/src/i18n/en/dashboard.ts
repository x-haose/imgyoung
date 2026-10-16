import type { dashboard as zh } from '../zh-CN/dashboard'

export const dashboard = {
  title: 'Dashboard',
  images: 'Images',
  storage: 'Storage used',
  storageHint: 'Original bytes in total, replicas not double counted',
  users: 'Users',
  accesses: 'Accesses',
  accessesHint:
    'Repeated hits on one image within one activity flush interval count once',
  trafficEstimate: 'Est. traffic {bytes}',
  pendingCleanup: 'Pending cleanup',
  pendingCleanupHint:
    'Images not accessed for a long time, waiting for you to keep or delete them',
  pendingDedup: 'Possible duplicates',
  pendingDedupHint: 'Similar-looking images waiting for a side-by-side review',
  used: 'Space used',
  quota: 'Quota',
  unlimited: 'Unlimited',
  quotaUsage: '{percent}% used',
  channels: 'Storage channels',
  channelsDesc:
    "Health and stored data per channel; bars show each channel's share of the total",
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
  offsiteMissingTitle: 'Your data only lives on this host',
  offsiteMissingDesc:
    'There is no healthy channel off this host’s disk, so image replicas and metadata snapshots exist only here; losing the host loses everything. Add an object storage or cloud drive channel, or find out why the one you added is unhealthy.',
  quickStartTitle: 'Quick start',
  quickStartDesc: 'No images yet. Three steps to get going',
  quickStartUserDesc:
    "You haven't uploaded any images yet. Two steps to get going",
  step1Title: 'Check storage channels',
  step1Desc:
    'The built-in local channel is ready to use; add S3, R2 or other cloud storage on the channels page.',
  step2Title: 'Issue an upload token',
  step2Desc:
    'Issue one token per client, such as PicGo or a script; the secret is shown only once.',
  step3Title: 'Start uploading',
  step3Desc:
    'Upload with curl, PicGo or any S3 client; the guide has ready-made configuration examples.',
  readGuide: 'Read the guide',
} satisfies typeof zh
