import { computed, nextTick } from 'vue'
import { fieldKey } from '../ConfigForm/ConfigForm.js'
import { usePagination } from '../ConfigPagination/ConfigPagination.js'

export { fieldKey }

export function useField(props, emit) {
  const id = computed(() => `config-field-${fieldKey(props.field).replaceAll('.', '-')}`)
  const descriptionId = computed(() => [
    props.field.hint && `${id.value}-hint`,
    props.error && `${id.value}-error`
  ].filter(Boolean).join(' ') || undefined)
  const entries = computed(() => Array.isArray(props.value) ? props.value : [])
  const pagination = usePagination(entries, 2)
  const selectOptions = computed(() => {
    const values = props.field.options || []
    const current = props.value ?? props.field.fallback ?? ''
    return values.some(item => item.value === current)
      ? values
      : [...values, { value: current, label: `当前值：${current === '' ? '未设置' : current}` }]
  })
  const update = value => {
    if (!props.disabled && !props.field.locked) emit('change', props.field.path, value)
  }
  const updateEntry = (index, value) => update(entries.value.map((entry, current) => current === index ? value : entry))
  const updateTarget = (index, key, value) => updateEntry(index, { ...entries.value[index], [key]: value ?? null })
  const removeEntry = index => update(entries.value.filter((_, current) => current !== index))
  const addEntry = async () => {
    update([...entries.value, props.field.type === 'network' ? { scheme: 'https', host: '', port: 443 } : ''])
    await nextTick()
    pagination.currentPage.value = pagination.pageCount.value
  }

  return { id, descriptionId, entries, selectOptions, update, updateEntry, updateTarget, removeEntry, addEntry, ...pagination }
}
