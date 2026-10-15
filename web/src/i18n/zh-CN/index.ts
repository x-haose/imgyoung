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
import { nav, navGroups } from './nav'
import { settings } from './settings'
import { tokens } from './tokens'
import { users } from './users'

/** 简体中文词典,也是全部语言的结构基准:其它语言须与之同型。 */
export const zhCN = {
  common,
  navGroups,
  nav,
  auth,
  errors,
  dashboard,
  images,
  tokens,
  channels,
  channelTemplates,
  users,
  dedup,
  cleanup,
  audit,
  guide,
  settings,
}

export type Messages = typeof zhCN
