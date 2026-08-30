import { ref } from 'vue'

export function usePage() {
  const loading = ref(false)

  const goLogin = () => window.location.assign('/#/login')

  return {
    loading,
    goLogin
  }
}