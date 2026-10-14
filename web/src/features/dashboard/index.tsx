import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Dashboard() {
  const m = useMessages()
  return <PlaceholderPage title={m.dashboard.title} />
}
