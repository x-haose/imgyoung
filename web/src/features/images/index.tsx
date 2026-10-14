import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Images() {
  const m = useMessages()
  return <PlaceholderPage title={m.images.title} />
}
