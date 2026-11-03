import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'

import { CaptureSettings } from './components/capture-settings'
import { CyberGuardSettings } from './components/cyber-guard-settings'
import { ExceptionLogsTable } from './components/exception-logs-table'

export function ExceptionLogs() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Exception Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid items-start gap-4 md:grid-cols-[7fr_3fr]'>
            <CyberGuardSettings />
            <CaptureSettings />
          </div>
          <ExceptionLogsTable />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
