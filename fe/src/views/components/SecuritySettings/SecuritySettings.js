import {onMounted, reactive, ref} from 'vue'

import {ElMessageBox, ElNotification} from 'element-plus'

import lang from '../../../i18n/i18n';

import {http} from '@/utils/axios';

import {normalizeCreationOptions, serializeCredential, webAuthnSupported} from '@/utils/webauthn';

export function usePage() {
  const supported = webAuthnSupported();
  
  const registering = ref(false);
  
  const credentialsLoading = ref(false);
  
  const credentials = ref([]);
  
  const ruleForm = reactive({current_pwd: '', new_pwd: '', new_pwd2: ''})
  
  const rules = reactive({
    current_pwd: [{required: true, message: lang.err_required_pwd, trigger: 'blur'}],
    new_pwd: [{required: true, message: lang.err_required_pwd, trigger: 'blur'}],
    new_pwd2: [{required: true, message: lang.err_required_pwd, trigger: 'blur'}],
  })
  
  const notify = (message, type = 'error') => {
    ElNotification({title: type === 'success' ? '操作成功' : '操作失败', message, type});
  }
  
  const loadCredentials = async () => {
    credentialsLoading.value = true;
    try {
      const res = await http.get('/api/security/webauthn/credentials');
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '加载安全密钥失败');
      credentials.value = Array.isArray(res.data) ? res.data : [];
    } catch (error) {
      notify(error?.message || '加载安全密钥失败');
    } finally {
      credentialsLoading.value = false;
    }
  }
  
  const registerCredential = async () => {
    if (!supported || registering.value) return;
    registering.value = true;
    try {
      const prompt = await ElMessageBox.prompt('请为新安全密钥设置一个名称', '注册安全密钥', {
        confirmButtonText: '确定', cancelButtonText: '取消', inputPlaceholder: '例如：工作电脑',
        inputValidator: value => String(value || '').trim().length > 0 || '请输入密钥名称',
      });
      const displayName = String(prompt.value).trim();
      const begin = await http.post('/api/security/webauthn/registration/begin', {});
      if (begin.errorNo !== 0) throw new Error(begin.errorMsg || '无法开始安全密钥注册');
      const credential = await navigator.credentials.create({publicKey: normalizeCreationOptions(begin.data.options)});
      const finish = await http.post('/api/security/webauthn/registration/finish', {
        session_id: begin.data.session_id,
        display_name: displayName,
        credential: serializeCredential(credential),
      });
      if (finish.errorNo !== 0) throw new Error(finish.errorMsg || '安全密钥注册失败');
      notify('安全密钥注册成功', 'success');
      await loadCredentials();
    } catch (error) {
      if (error !== 'cancel' && error !== 'close' && error?.name !== 'NotAllowedError') {
        notify(error?.message || '安全密钥注册失败');
      }
    } finally {
      registering.value = false;
    }
  }
  
  const renameCredential = async item => {
    try {
      const prompt = await ElMessageBox.prompt('请输入新的密钥名称', '重命名', {
        confirmButtonText: '确定', cancelButtonText: '取消', inputValue: item.display_name,
        inputValidator: value => String(value || '').trim().length > 0 || '请输入密钥名称',
      });
      const res = await http.post('/api/security/webauthn/credentials/rename', {
        id: item.id, display_name: String(prompt.value).trim(),
      });
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '密钥重命名失败');
      notify('密钥重命名成功', 'success');
      await loadCredentials();
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '密钥重命名失败');
    }
  }
  
  const revokeCredential = async item => {
    try {
      await ElMessageBox.confirm(`确定要撤销密钥“${item.display_name}”吗？撤销后将不能用于登录`, '撤销安全密钥', {
        confirmButtonText: '确定撤销', cancelButtonText: '取消', type: 'warning',
      });
      const res = await http.post('/api/security/webauthn/credentials/revoke', {id: item.id});
      if (res.errorNo !== 0) throw new Error(res.errorMsg || '撤销安全密钥失败');
      notify('安全密钥已撤销', 'success');
      await loadCredentials();
    } catch (error) {
      if (error !== 'cancel' && error !== 'close') notify(error?.message || '撤销安全密钥失败');
    }
  }
  
  const formatTime = value => value ? new Date(value).toLocaleString('zh-CN', {hour12: false}) : '-';
  
  const logout = () => http.post('/api/logout', {}).then(() => location.reload());
  
  const submit = () => {
    if (!ruleForm.current_pwd || !ruleForm.new_pwd) return;
    if (ruleForm.new_pwd !== ruleForm.new_pwd2) {
      notify(lang.err_pwd_diff);
      return;
    }
    http.post('/api/settings/modify_password', {
      current_password: ruleForm.current_pwd,
      password: ruleForm.new_pwd,
    }).then(res => {
      notify(res.errorNo === 0 ? res.data : res.errorMsg, res.errorNo === 0 ? 'success' : 'error');
      if (res.errorNo === 0) Object.assign(ruleForm, {current_pwd: '', new_pwd: '', new_pwd2: ''});
    });
  }
  
  onMounted(loadCredentials);

  return {
    onMounted,
    reactive,
    ref,
    ElMessageBox,
    ElNotification,
    lang,
    http,
    normalizeCreationOptions,
    serializeCredential,
    webAuthnSupported,
    supported,
    registering,
    credentialsLoading,
    credentials,
    ruleForm,
    rules,
    notify,
    loadCredentials,
    registerCredential,
    renameCredential,
    revokeCredential,
    formatTime,
    logout,
    submit
  }
}
