import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Channels() {
  const m = useMessages()
  return <PlaceholderPage title={m.channels.title} />
}
