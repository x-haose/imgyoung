import { t } from '@/i18n'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { getPageNumbers } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Button } from '@/components/ui/button'

type GridPaginationProps = {
  page: number
  pageCount: number
  onChange: (page: number) => void
}

/** 卡片网格的分页条:固定每页张数,只有翻页;单页时不渲染。 */
export function GridPagination({
  page,
  pageCount,
  onChange,
}: GridPaginationProps) {
  const m = useMessages()
  if (pageCount <= 1) return null
  return (
    <nav
      className='flex flex-wrap items-center justify-center gap-1'
      aria-label={t(m.common.pageOf, { page, total: pageCount })}
    >
      <Button
        variant='outline'
        size='icon'
        className='size-8'
        disabled={page <= 1}
        onClick={() => onChange(page - 1)}
      >
        <ChevronLeft />
        <span className='sr-only'>{m.common.prevPage}</span>
      </Button>
      {getPageNumbers(page, pageCount).map((n, i) =>
        n === '...' ? (
          <span key={`gap-${i}`} className='px-1 text-sm text-muted-foreground'>
            …
          </span>
        ) : (
          <Button
            key={n}
            variant={n === page ? 'default' : 'outline'}
            className='h-8 min-w-8 px-2'
            aria-current={n === page ? 'page' : undefined}
            onClick={() => onChange(n as number)}
          >
            {n}
          </Button>
        )
      )}
      <Button
        variant='outline'
        size='icon'
        className='size-8'
        disabled={page >= pageCount}
        onClick={() => onChange(page + 1)}
      >
        <ChevronRight />
        <span className='sr-only'>{m.common.nextPage}</span>
      </Button>
    </nav>
  )
}
