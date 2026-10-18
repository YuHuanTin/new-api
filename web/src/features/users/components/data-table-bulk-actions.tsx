/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { Table } from '@tanstack/react-table'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import { deleteUsersBatch } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import type { User } from '../types'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { requestVerification, verificationActive, triggerRefresh } = useUsers()
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)

  const selectedIds = table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original.id)

  const handleDelete = async () => {
    setIsDeleting(true)
    try {
      const proof = await requestVerification({
        scope: 'admin.user.batch_delete',
        context: { user_ids: selectedIds },
        title: t('Verify to delete selected users'),
        description: t(
          'Confirm your identity before permanently deleting {{count}} selected users.',
          { count: selectedIds.length }
        ),
      })
      if (!proof) return
      const result = await deleteUsersBatch(selectedIds, proof.proof_token)
      if (result.success) {
        toast.success(t(SUCCESS_MESSAGES.USERS_DELETED))
        setShowDeleteConfirm(false)
        table.resetRowSelection()
        triggerRefresh()
      } else {
        handleServerError(result, t(ERROR_MESSAGES.DELETE_FAILED))
      }
    } catch (error) {
      handleServerError(
        AuthOperationError.from(error),
        t(ERROR_MESSAGES.UNEXPECTED)
      )
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName='user'>
        <Button
          variant='destructive'
          size='sm'
          onClick={() => setShowDeleteConfirm(true)}
        >
          <Trash2 />
          {t('Delete selected users')}
        </Button>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={showDeleteConfirm && !verificationActive}
        onOpenChange={(open) => !open && setShowDeleteConfirm(false)}
        title={t('Are you sure?')}
        desc={t(
          'This will permanently delete {{count}} selected users. This action cannot be undone.',
          { count: selectedIds.length }
        )}
        confirmText={isDeleting ? t('Deleting...') : t('Delete')}
        destructive
        isLoading={isDeleting}
        handleConfirm={handleDelete}
      />
    </>
  )
}
