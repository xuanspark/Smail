if (window.location.pathname === '/rest' || window.location.pathname === '/rest/') {
  import('./public-recovery.js')
} else {
  import('./main-app.js')
}
