import { createFileRoute } from '@tanstack/react-router'
import { Guide } from '@/features/guide'

export const Route = createFileRoute('/_authenticated/guide/')({
  component: Guide,
})
