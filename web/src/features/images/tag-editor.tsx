import { useId, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { toast } from 'sonner'
import { useMessages } from '@/context/locale-provider'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  MAX_TAG_LENGTH,
  MAX_TAGS_PER_IMAGE,
  parseTags,
  tagsQueryOptions,
} from './tags'

type TagEditorProps = {
  tags: string[]
  disabled: boolean
  onChange: (tags: string[]) => void // 提交整组标签
}

/** 一张图的标签:已有标签带删除键,输入框回车或失焦时追加,联想用户已有的标签。 */
export function TagEditor({ tags, disabled, onChange }: TagEditorProps) {
  const m = useMessages().images
  const listId = useId()
  const [text, setText] = useState('')
  const known = useQuery(tagsQueryOptions())

  const commit = () => {
    const added = parseTags(text).filter((name) => !tags.includes(name))
    setText('')
    if (added.length === 0) return
    if (tags.length + added.length > MAX_TAGS_PER_IMAGE) {
      toast.warning(m.tagsTooMany.replace('{max}', String(MAX_TAGS_PER_IMAGE)))
      return
    }
    onChange([...tags, ...added])
  }

  return (
    <div className='space-y-2'>
      {tags.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {tags.map((name) => (
            <Badge key={name} variant='secondary' className='gap-1 pe-1'>
              {name}
              <button
                type='button'
                disabled={disabled}
                className='rounded-sm opacity-60 transition-opacity hover:opacity-100'
                aria-label={`${m.tagRemove} ${name}`}
                onClick={() => onChange(tags.filter((t) => t !== name))}
              >
                <X className='size-3' />
              </button>
            </Badge>
          ))}
        </div>
      )}
      <Input
        value={text}
        disabled={disabled}
        maxLength={MAX_TAG_LENGTH * 4}
        list={listId}
        placeholder={m.tagsPlaceholder}
        className='h-8'
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            commit()
          }
        }}
        onBlur={commit}
      />
      <datalist id={listId}>
        {(known.data?.items ?? [])
          .filter((t) => !tags.includes(t.name))
          .map((t) => (
            <option key={t.name} value={t.name} />
          ))}
      </datalist>
    </div>
  )
}
