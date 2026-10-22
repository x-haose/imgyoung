import type { Messages } from '../zh-CN'
import { audit } from './audit'
import { auth } from './auth'
import { channelTemplates } from './channel-templates'
import { channels } from './channels'
import { cleanup } from './cleanup'
import { common } from './common'
import { dashboard } from './dashboard'
import { dedup } from './dedup'
import { errors } from './errors'
import { guide } from './guide'
import { images } from './images'
import { imports } from './imports'
import { nav, navGroups } from './nav'
import { server } from './server'
import { settings } from './settings'
import { tokens } from './tokens'
import { users } from './users'

/** 英文词典,结构与简体中文词典逐键对应。 */
export const en = {
  common,
  navGroups,
  nav,
  auth,
  errors,
  dashboard,
  images,
  imports,
  tokens,
  channels,
  channelTemplates,
  users,
  dedup,
  cleanup,
  audit,
  guide,
  settings,
  server,
} satisfies Messages
