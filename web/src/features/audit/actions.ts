/** 后端 audit 包记录的全部动作标识;筛选项与标签映射都以此为准。 */
export const AUDIT_ACTIONS = [
  'channel.create',
  'channel.update',
  'channel.delete',
  'channel.export',
  'token.issue',
  'token.revoke',
  'token.rotate',
  'token.delete',
  'user.create',
  'user.update',
  'user.delete',
  'image.update',
  'image.delete',
  'image.recover',
  'data.export',
  'backup.create',
  'backup.restore',
  'dedup.merge',
  'dedup.dismiss',
  'referer.add',
  'referer.remove',
  'config.update',
] as const

export type AuditAction = (typeof AUDIT_ACTIONS)[number]
