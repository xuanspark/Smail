import { usePage } from '../ConfigSettings.js'

export function useMaintenance(pageKey) {
  const page = usePage(pageKey)
  const selectBackup = generation => {
    if (page.working.value) return
    page.rollbackGeneration.value = generation
    page.rollbackPreview.value = null
    page.rollbackConfirmed.value = false
  }
  return { ...page, selectBackup }
}
