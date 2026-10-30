import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import { ExceptionLogs } from '@/features/exception-logs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const exceptionLogsSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  error_code: z.string().optional().catch(''),
})

export const Route = createFileRoute('/_authenticated/exception-logs/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: exceptionLogsSearchSchema,
  component: ExceptionLogs,
})
