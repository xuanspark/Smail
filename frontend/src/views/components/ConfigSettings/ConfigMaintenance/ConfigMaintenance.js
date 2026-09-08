import { usePage } from '../ConfigSettings.js'
import { usePagination } from '../ConfigPagination/ConfigPagination.js'

export function useMaintenance(pageKey) {
  const page = usePage(pageKey)
  const selectBackup = generation => {
    if (page.working.value) return
    page.rollbackGeneration.value = generation
    page.rollbackPreview.value = null
    page.rollbackConfirmed.value = false
  }
  const backupPagination = usePagination(page.backups, 6)
  return { ...page, selectBackup, backupPagination }
}
