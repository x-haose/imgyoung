import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Dedup() {
  const m = useMessages()
  return <PlaceholderPage title={m.dedup.title} />
}
