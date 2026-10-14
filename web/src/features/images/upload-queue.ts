import { useEffect, useState, useSyncExternalStore } from 'react'
import { errorMessage } from '@/lib/errors'
import { uploadFile, uploadFromURL, type UploadOptions } from './data'

/** 单个上传条目的展示状态。 */
export interface UploadEntry {
  uid: number
  name: string // 文件名或转存地址
  status: 'uploading' | 'done' | 'error'
  url?: string // 成功后的绝对访问地址
  error?: string // 失败时的后端文案
}

/** 同时进行的上传数上限;解码在服务端,前端只需避免一次性塞爆并发闸。 */
const CONCURRENCY = 2

type Job = { uid: number; file: File; name: string; opts: UploadOptions }

/**
 * 上传队列:React 之外的小型存储,受控并发逐个走后台会话上传端点,转存单独一条、不占文件并发。
 * onSettled 在一批文件全部落定且至少一张成功、或一次转存成功时调用,供调用方刷新列表。
 */
function createUploadQueue() {
  let onSettled = () => {}
  let entries: UploadEntry[] = []
  const listeners = new Set<() => void>()
  const pending: Job[] = []
  let nextUid = 0
  let running = 0
  let anySucceeded = false

  const set = (next: UploadEntry[]) => {
    entries = next
    listeners.forEach((l) => l())
  }
  const patch = (uid: number, changes: Partial<UploadEntry>) =>
    set(entries.map((e) => (e.uid === uid ? { ...e, ...changes } : e)))
  const add = (name: string): number => {
    const uid = nextUid++
    set([...entries, { uid, name, status: 'uploading' }])
    return uid
  }

  const pump = () => {
    while (running < CONCURRENCY && pending.length > 0) {
      const job = pending.shift()
      if (!job) return
      running += 1
      uploadFile(job.file, job.name, job.opts)
        .then((res) => {
          anySucceeded = true
          patch(job.uid, { status: 'done', url: res.url })
        })
        .catch((err: unknown) => {
          patch(job.uid, { status: 'error', error: errorMessage(err) })
        })
        .finally(() => {
          running -= 1
          if (running === 0 && pending.length === 0 && anySucceeded) {
            anySucceeded = false
            onSettled()
          }
          pump()
        })
    }
  }

  return {
    setOnSettled: (handler: () => void) => {
      onSettled = handler
    },
    subscribe: (listener: () => void) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    snapshot: () => entries,
    enqueueFile: (file: File, name: string, opts: UploadOptions) => {
      pending.push({ uid: add(name), file, name, opts })
      pump()
    },
    /** 让服务端抓取远端图片并落库;返回是否成功。 */
    fetchURL: async (url: string, opts: UploadOptions): Promise<boolean> => {
      const uid = add(url)
      try {
        const res = await uploadFromURL(url, opts)
        patch(uid, { status: 'done', url: res.url })
        onSettled()
        return true
      } catch (err) {
        patch(uid, { status: 'error', error: errorMessage(err) })
        return false
      }
    },
    /** 清掉已落定的条目,进行中的保留。 */
    clearSettled: () => set(entries.filter((e) => e.status === 'uploading')),
  }
}

/** 在组件内持有一条上传队列;队列跨渲染存活,onSettled 始终调用最新的回调。 */
export function useUploadQueue(onSettled: () => void) {
  const [queue] = useState(createUploadQueue)
  useEffect(() => queue.setOnSettled(onSettled), [queue, onSettled])
  const entries = useSyncExternalStore(queue.subscribe, queue.snapshot)
  return {
    entries,
    enqueueFile: queue.enqueueFile,
    fetchURL: queue.fetchURL,
    clearSettled: queue.clearSettled,
  }
}
