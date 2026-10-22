import { createFileRoute } from '@tanstack/react-router'
import { ImportDetail } from '@/features/imports/detail'

export const Route = createFileRoute('/_authenticated/imports/$id')({
  component: ImportDetail,
})
