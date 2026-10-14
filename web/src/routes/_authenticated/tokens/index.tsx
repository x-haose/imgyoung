import { createFileRoute } from '@tanstack/react-router'
import { Tokens } from '@/features/tokens'

export const Route = createFileRoute('/_authenticated/tokens/')({
  component: Tokens,
})
