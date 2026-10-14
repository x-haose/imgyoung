import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Audit() {
  const m = useMessages()
  return <PlaceholderPage title={m.audit.title} />
}
