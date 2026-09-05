import './ResetBlocked.js'

export function usePage() {
  const goLogin = () => {
    window.location.assign('/#/login')
  }

  return {
    goLogin
  }
}
