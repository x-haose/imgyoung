import type { channels as zh } from '../zh-CN/channels'

export const channels = {
  title: 'Storage channels',
  description:
    'Uploads go to the healthy channel with the highest priority first; the other channels are filled in automatically in the background',
  create: 'New channel',
  edit: 'Edit channel',
  colChannel: 'Channel',
  colPriority: 'Priority',
  colStatus: 'Status',
  colReplicas: 'Replicas',
  primary: 'Primary',
  primaryHint:
    'The healthy channel with the highest priority; uploads are written here first',
  encrypted: 'Encrypted',
  directLink: 'Direct link',
  healthy: 'Healthy',
  unhealthy: 'Unhealthy',
  deleting: 'Draining',
  deletingHint:
    'Objects on this channel are being deleted; it disappears once they are gone',
  probeFailedHint: 'The last probe failed',
  neverChecked: 'Not probed yet',
  checkedAt: 'Last probe: {time}',
  checkedAgo: 'probed {ago}',
  replicaDone: '{n} ready',
  replicaPending: '{n} pending',
  replicaFailed: '{n} failed',
  replicaDoneHint: '{n} replicas are ready',
  replicaPendingHint: '{n} replicas are pending or being copied',
  replicaFailedHint:
    '{n} replicas failed to replicate; periodic reconciliation retries them while the channel is healthy. Check the channel config if it keeps failing',
  actionCheck: 'Probe again',
  actionRemoveNow: 'Remove now',
  actionExportConfig: 'Export rclone config',
  actionsMenu: 'Channel actions',
  checkPassed: 'Channel {name} passed the probe',
  checkFailed: 'Channel {name} failed the probe: {error}',
  empty: 'No storage channels yet',
  emptyHint: 'Create a channel before uploading images',
  soloLocalTitle: 'Only the local disk is configured',
  soloLocalDesc:
    'Images and metadata snapshots exist only on this host, so losing the host loses everything. Add an object storage or cloud drive channel and existing images are replicated to it automatically.',
  deleteTitle: 'Delete channel {name}',
  deleteDesc:
    'If any image has no ready replica on another channel, the deletion is refused.',
  purgeLabel: 'Also delete what this service stored on the channel',
  purgeOnHint:
    'Image replicas and metadata snapshots are deleted one by one in the background; the channel shows as draining until they are gone. Files this service did not write are left alone. Untick this if the credentials are already revoked or the bucket no longer exists.',
  purgeOffHint:
    'The channel is removed immediately; the image replicas and metadata snapshots on it are left as they are.',
  deleteAndPurge: 'Delete and purge',
  removeOnly: 'Remove only',
  deletedPurging:
    'The channel is draining; it is removed once its objects are gone',
  deletedKept: 'Channel removed; remote objects were kept',
  removeNowTitle: 'Remove channel {name} now?',
  removeNowDesc:
    'Stops draining and removes the channel immediately; objects not yet deleted are left as they are.',
  exportConfigTitle: 'Export rclone config',
  exportConfigDesc:
    "An rclone config file for channel {name}. With it, a stock rclone can read and write this channel's data without ImgYoung.",
  exportConfigBackend:
    'Backend remote: the same parameters the channel uses, including access credentials and OAuth tokens',
  exportConfigCrypt:
    'Crypt remote: the same settings as the server-side encryption, so reads come back as plaintext',
  exportConfigUsage:
    'Comments at the top of the file describe each remote and show example commands.',
  exportConfigWarnEncrypted: 'Contains credentials and decryption passwords',
  exportConfigWarnPlain: "Contains the channel's credentials",
  exportConfigWarnDesc:
    "Keep it offline and safe, just like the master key: anyone who has it can read and write all of this channel's data.",
  exportConfigDownload: 'Download config file',
  pickType: 'Choose a storage type',
  groupObjectStorage: 'Object storage',
  groupDrive: 'Cloud drives',
  groupAdvanced: 'Advanced',
  customName: 'Custom',
  customDesc: 'Enter rclone parameters by hand',
  nameLabel: 'Channel name',
  namePlaceholder: 'e.g. r2-primary',
  nameRequired: 'Enter a channel name',
  nameTooLong: 'The name may be at most 64 bytes',
  fieldRequired: '{label} is required',
  selectPlaceholder: 'Select…',
  secretKeepHint:
    'Leave as is to keep the stored value; type a new value to replace it',
  driverLabel: 'rclone backend',
  driverPlaceholder: 's3 / local / webdav …',
  driverRequired: 'Enter a backend name',
  configLabel: 'Backend parameters',
  configHint:
    'Parameters of the rclone backend, such as path for local or access_key_id for S3, with public_base_url for the public URL; secrets may be plain text or rclone obscure output',
  configKeepHint:
    'A parameter whose value is {placeholder} keeps the stored secret; type a new value to replace it',
  configKeyPlaceholder: 'Key (e.g. path)',
  configValuePlaceholder: 'Value',
  configKeyRequired: 'The key must not be empty',
  configKeyDuplicate: 'Duplicate key',
  addParam: 'Add parameter',
  removeParam: 'Remove this parameter',
  priorityLabel: 'Priority',
  priorityHint: '0–1000; higher wins',
  priorityRange: 'Priority must be an integer from 0 to 1000',
  encryptLabel: 'Server-side encryption',
  encryptInherit: 'Inherit the global default',
  encryptOn: 'Force on',
  encryptOff: 'Force off',
  encryptOnDirectLink:
    'A channel with 302 direct links cannot be encrypted; turn direct links off to encrypt it',
  encryptInheritDirectLink:
    'Direct links are on and the global default is to encrypt, so only Force off is available',
  encryptedYes: 'Encrypted',
  encryptedNo: 'Not encrypted',
  encryptedImmutable:
    'Cannot be changed after creation; to change it, create a new channel and migrate the data',
  directLinkLabel: 'Allow 302 direct links',
  directLinkHint:
    'In redirect access mode, original-image requests are redirected (302) to the public bucket URL instead of passing through this site',
  directLinkEncrypted: 'Encrypted channels cannot use 302 direct links',
  directLinkInheritEncrypted:
    'New channels are encrypted by default, and encrypted channels cannot use 302 direct links; set Server-side encryption to Force off to use them',
  directLinkNeedsBaseURL:
    'The public URL (public_base_url) is not set, so direct links will not take effect and images are still served by this service',
  autoCreateBucketLabel: 'Create the bucket automatically',
  autoCreateBucketHint:
    'Tries to create the bucket when the channel is created; the key needs bucket-creation rights, and a failure aborts the creation. Turn it off if the bucket already exists',
  testConnection: 'Test connection',
  testOk: 'Connection OK (write, read and delete probes passed)',
  testFailed: 'Connection failed: {error}',
  testTokenRefreshed:
    'The provider refreshed the authorization token during the test. The form now holds the new token, so just save; the old one may no longer work.',
  saveTokenRefreshed:
    'Saving failed, but the provider refreshed the authorization token meanwhile. The form now holds the new token; fix the problem and save again, no need to authorize again.',
  createSubmit: 'Create',
  saved: 'Channel saved',
  created: 'Channel created',
  savedProbeFailed: 'Channel saved, but the probe failed: {error}',
  createdProbeFailed: 'Channel created, but the probe failed: {error}',
  oauthOneClick: 'One-click authorization',
  oauthPaste: 'Paste a token',
  oauthGo: 'Authorize with {name}',
  oauthWaiting: 'Waiting for authorization…',
  oauthCancel: 'Stop waiting',
  oauthHint:
    'Register your own app with the provider, set {path} on this site as its callback URL and fill in the app client ID and secret above; the public site address must also be set in Settings.',
  oauthPastePlaceholder: 'Paste the output of rclone authorize',
  oauthPasteHint:
    'Run {command} on any machine with a browser, finish the authorization and paste the whole output here. This works even if this site has no public domain.',
  oauthAuthorized: 'Authorized',
  oauthKept: 'using the stored token',
  oauthReauthorize: 'Authorize again',
  oauthTimeout: 'Timed out waiting for authorization, please start again',
} satisfies typeof zh
