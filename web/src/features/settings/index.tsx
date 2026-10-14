import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Settings() {
  const m = useMessages()
  return <PlaceholderPage title={m.settings.title} />
}
