import { type UploadOptions } from '@/features/images/data'
import {
  defaultOptionsDraft,
  type OptionsDraft,
} from '@/features/images/upload-options'
import { type ImportCreateRequest, type ImportMode } from './data'

/** 一份待扫描的文档:文件名与读出的全文。 */
export type SourceDocument = { name: string; text: string }

/** 新建导入表单的草稿;各模式的输入互不影响,提交时只取当前模式的字段。 */
export interface CreateDraft {
  mode: ImportMode
  urls: string // 图片地址,一行一个
  sites: string // 站点地图、订阅源或网页地址,一行一个
  text: string // 粘贴的 Markdown 或 HTML
  files: SourceDocument[] // 选择的文件
  baseURL: string // 解析文本里相对路径的基准地址
  referer: string // 抓取图片时发送的 Referer
  options: OptionsDraft
}

export const emptyDraft: CreateDraft = {
  mode: 'images',
  urls: '',
  sites: '',
  text: '',
  files: [],
  baseURL: '',
  referer: '',
  options: defaultOptionsDraft,
}

/** 按行切开并去掉首尾空白,丢弃空行。 */
function lines(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l !== '')
}

/** 当前模式下是否已有可提交的输入。 */
export function hasInput(d: CreateDraft): boolean {
  switch (d.mode) {
    case 'images':
      return lines(d.urls).length > 0
    case 'site':
      return lines(d.sites).length > 0
    case 'text':
      return d.text.trim() !== '' || d.files.length > 0
  }
}

/**
 * 草稿转创建请求,只带当前模式的字段;调用方须先确认 hasInput。
 * pastedName 是粘贴文本作为文档时的名字。
 */
export function toRequest(
  d: CreateDraft,
  options: UploadOptions,
  pastedName: string
): ImportCreateRequest {
  const common = {
    ...(options.ttl !== undefined ? { ttl: options.ttl } : {}),
    ...(options.exif !== undefined ? { exif_policy: options.exif } : {}),
    ...(d.referer.trim() !== '' ? { referer: d.referer.trim() } : {}),
  }
  switch (d.mode) {
    case 'images':
      return { mode: 'images', urls: lines(d.urls), ...common }
    case 'site':
      return { mode: 'site', sites: lines(d.sites), ...common }
    case 'text': {
      const documents: SourceDocument[] = [...d.files]
      if (d.text.trim() !== '') {
        documents.unshift({ name: pastedName, text: d.text })
      }
      return {
        mode: 'text',
        documents,
        ...(d.baseURL.trim() !== '' ? { base_url: d.baseURL.trim() } : {}),
        ...common,
      }
    }
  }
}
