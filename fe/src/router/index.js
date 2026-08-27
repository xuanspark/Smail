import { createRouter, createWebHashHistory } from 'vue-router'
import ListView from '../views/List/List.vue'
import EditerView from '../views/Editer/Editer.vue'
import LoginView from '../views/Login/Login.vue'
import EmailDetailView from '../views/EmailDetail/EmailDetail.vue'
import SetupView from '../views/Setup/Setup.vue'
import PasswordRecoveryView from '../views/PasswordRecovery/PasswordRecovery.vue'
import ResetBlockedView from '../views/ResetBlocked/ResetBlocked.vue'


const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: ListView
    },
    {
      path: '/list',
      name: 'list',
      component: ListView
    },
    {
      path: '/editer',
      name: "editer",
      component: EditerView
    },
    {
      path: '/login',
      name: "login",
      component: LoginView
    },
    {
      path: '/setup',
      name: "setup",
      component: SetupView
    },
    {
      path: '/forgot-password',
      name: "forgot-password",
      component: PasswordRecoveryView
    },
    {
      path: '/reset-blocked',
      name: 'reset-blocked',
      component: ResetBlockedView
    },
    {
      path: '/detail/:id',
      name: "detail",
      component: EmailDetailView
    }
  ]
})





export {router};
