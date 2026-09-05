export const submenuProps = {
  title: { type: String, required: true },
  caption: { type: String, default: '设置选项' },
  items: { type: Array, required: true },
  modelValue: { type: String, required: true }
}

export function useSubmenu(props, emit) {
  function select(value) {
    if (value !== props.modelValue && props.items.some(item => item.value === value)) {
      emit('select', value)
    }
  }

  return { select }
}
