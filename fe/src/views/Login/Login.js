import {reactive, ref} from 'vue'

import {ElMessage} from 'element-plus'

import {useRouter} from 'vue-router'

import lang from '../../i18n/i18n';

import {http} from "@/utils/axios";

import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

export function usePage() {
  const router = useRouter()
  const globalStatus = useGlobalStatusStore();
  
  const loading = ref(false);
  
  const form = reactive({
    account: '',
    password: '',
  })
  
  const onSubmit = () => {
    if (!form.account || !form.password) {
      ElMessage.warning(lang.login_fill_required);
      return;
    }
    loading.value = true;
    http.post("/api/login", form).then(res => {
      loading.value = false;
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        Object.assign(globalStatus.userInfos , res.data) 
        router.replace({
          path: '/',
          query: {
            redirect: router.currentRoute.fullPath
          }
        })
      }
    }).catch(() => {
      loading.value = false;
    })
  }

  const goForgotPassword = () => {
    router.push({name: 'forgot-password'})
  }

  return {
    reactive,
    ref,
    ElMessage,
    router,
    lang,
    http,
    useGlobalStatusStore,
    globalStatus,
    loading,
    form,
    onSubmit,
    goForgotPassword
  }
}
