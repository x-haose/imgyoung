import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Guide() {
  const m = useMessages()
  return <PlaceholderPage title={m.guide.title} />
}
