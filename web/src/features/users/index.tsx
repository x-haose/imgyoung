import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Users() {
  const m = useMessages()
  return <PlaceholderPage title={m.users.title} />
}
