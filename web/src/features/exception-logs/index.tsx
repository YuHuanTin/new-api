import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { CaptureSettings } from './components/capture-settings'
import { CyberGuardSettings } from './components/cyber-guard-settings'
import { ExceptionLogsTable } from './components/exception-logs-table'

export function ExceptionLogs() {
  const { t } = useTranslation()
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Exception Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div
            className={cn(
              'grid items-start gap-4',
              isRoot && 'md:grid-cols-[7fr_3fr]'
            )}
          >
            <CyberGuardSettings />
            <CaptureSettings />
          </div>
          <ExceptionLogsTable />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
