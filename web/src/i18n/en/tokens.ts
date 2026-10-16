import type { tokens as zh } from '../zh-CN/tokens'

export const tokens = {
  title: 'Upload tokens',
  description:
    'Issue a named token for each client (PicGo, scripts, CLI); the secret is shown only once, at issue time',
  issue: 'Issue token',
  issueFirst: 'Issue the first token',
  viewGuide: 'Open the guide',
  emptyTitle: 'No tokens yet',
  emptyDesc:
    'Issue one for PicGo or a script before any client can call the upload API',
  emptyOther: 'This user has no tokens',
  viewingUser: 'View by user',
  self: 'me',
  columns: {
    name: 'Name',
    prefix: 'Prefix',
    status: 'Status',
    expires: 'Expires',
    lastUsed: 'Last used',
    created: 'Issued',
  },
  status: { active: 'Active', expired: 'Expired', revoked: 'Revoked' },
  never: 'Never',
  neverUsed: 'Never used',
  actions: {
    more: 'More actions',
    s3: 'S3 client settings',
    rotate: 'Rotate',
    revoke: 'Revoke',
    delete: 'Delete record',
  },
  create: {
    title: 'Issue an upload token',
    name: 'Purpose',
    namePlaceholder: 'picgo',
    nameHint:
      'e.g. picgo or blog-script; an active token with the same name cannot exist twice',
    nameRequired: 'Enter a purpose name',
    nameTooLong: 'At most {max} characters',
    expiry: 'Expiry',
    expiryHint:
      'After expiry the token stops working, exactly as if it were revoked',
    forever: 'Never',
    days: '{days} days',
    submit: 'Issue',
  },
  issued: {
    title: 'Token issued',
    rotatedTitle: 'Token rotated',
    warning:
      'The secret is shown only this once; it cannot be viewed again after closing',
    usage: 'Usage: request header Authorization: Bearer <token>',
    confirmSaved: 'I have stored this secret safely',
    close: 'Close',
  },
  rotate: {
    title: 'Rotate this token?',
    desc: 'A new token with the same name replaces it; the old one stops working immediately and the new secret is shown only once.',
    confirm: 'Rotate',
  },
  revoke: {
    title: 'Revoke this token?',
    desc: 'Clients using it can no longer upload; its S3 credential stops working as well.',
    confirm: 'Revoke',
    done: 'Token revoked',
  },
  delete: {
    title: 'Delete this record?',
    desc: 'The inactive record leaves the list; the audit trail is kept.',
    confirm: 'Delete',
    done: 'Record deleted',
  },
  s3: {
    title: 'S3 client settings · {name}',
    intro:
      'Pick the "AWS S3" uploader in PicList, or in PicGo after installing its S3 plugin (Typora uploads through PicGo), then fill in the fields below',
    note: 'The secret is derived from the master key and can be viewed here any time; revoking the token invalidates it.',
    pathStyle:
      'Path-style addressing: a self-hosted server has no bucket subdomains, so enable "path style" if the client has such a switch.',
    copy: 'Copy',
    fields: {
      access_key_id: 'Access key ID',
      secret_access_key: 'Secret access key',
      bucket: 'Bucket',
      region: 'Region',
      endpoint: 'Custom endpoint',
      url_prefix: 'Custom domain',
    },
    hints: {
      bucket: 'This server has no buckets; any name is accepted',
      region: 'Regions are not distinguished; keep the default',
      endpoint:
        'The trailing /s3 is the path prefix of the S3-compatible API and must stay',
      url_prefix:
        'Clients build the final image URL from it; it includes the user id segment',
    },
  },
} satisfies typeof zh
