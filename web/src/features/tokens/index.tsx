import { useMessages } from '@/context/locale-provider'
import { PlaceholderPage } from '@/features/placeholder'

export function Tokens() {
  const m = useMessages()
  return <PlaceholderPage title={m.tokens.title} />
}
