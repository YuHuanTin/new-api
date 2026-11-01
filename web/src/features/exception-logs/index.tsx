import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'

import { CaptureSettings } from './components/capture-settings'
import { CyberGuardSettings } from './components/cyber-guard-settings'
import { ExceptionLogsTable } from './components/exception-logs-table'

export function ExceptionLogs() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Exception Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <div className='grid items-start gap-4 md:grid-cols-[7fr_3fr]'>
            <CyberGuardSettings />
            <CaptureSettings />
          </div>
          <div className='min-h-0 flex-1'>
            <ExceptionLogsTable />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
