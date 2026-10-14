import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Cleanup() {
  const m = useMessages()
  return <PlaceholderPage title={m.cleanup.title} />
}
