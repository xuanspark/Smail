import { ref } from 'vue'
import { defineStore } from 'pinia'
import lang from '../i18n/i18n';

const useGroupStore = defineStore('group', () => {
  const tag = ref("")
  const name = ref(lang.inbox)
  const keyword = ref("")
  const revision = ref(0)
  const invalidate = () => { revision.value += 1 }
  return { tag, name, keyword, revision, invalidate }
})

export default useGroupStore