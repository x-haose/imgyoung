import { useMessages } from '@/context/locale-provider'
import { Page } from '@/components/layout/page'
import { Card, CardContent } from '@/components/ui/card'

/** 尚未重做的页面占位。 */
export function PlaceholderPage({ title }: { title: string }) {
  const m = useMessages()
  return (
    <Page title={title}>
      <Card>
        <CardContent className='py-16 text-center text-sm text-muted-foreground'>
          {m.common.comingSoon}
        </CardContent>
      </Card>
    </Page>
  )
}
