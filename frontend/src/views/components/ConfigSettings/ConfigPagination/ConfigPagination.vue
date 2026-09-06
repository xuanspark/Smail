<template>
  <nav v-if="pageCount > 1" class="config-pagination" :aria-label="label">
    <span aria-live="polite">共 {{ total }} 项 · {{ currentPage }} / {{ pageCount }} 页</span>
    <el-button size="small" :disabled="disabled || currentPage <= 1" :aria-label="`${label}上一页`" @click="emit('update:currentPage', currentPage - 1)">上一页</el-button>
    <el-button size="small" :disabled="disabled || currentPage >= pageCount" :aria-label="`${label}下一页`" @click="emit('update:currentPage', currentPage + 1)">下一页</el-button>
  </nav>
</template>

<script setup>
defineProps({
  currentPage: { type: Number, required: true },
  pageCount: { type: Number, required: true },
  total: { type: Number, required: true },
  label: { type: String, default: '列表分页' },
  disabled: Boolean
})
const emit = defineEmits(['update:currentPage'])
</script>

<style scoped src="./ConfigPagination.css"></style>
