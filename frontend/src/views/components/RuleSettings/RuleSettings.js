import {computed, reactive, ref} from 'vue';

import lang from '../../../i18n/i18n';

import {Delete, Edit, InfoFilled, Plus} from '@element-plus/icons-vue'

import {http} from "@/utils/axios";

import {ElNotification} from "element-plus";
import {useGlobalStatusStore} from '@/stores/useGlobalStatusStore'

export function usePage() {
  const globalStatus = useGlobalStatusStore()
  const canReadGlobalPolicy = computed(() => globalStatus.hasCapability('config.read'))
  const data = ref([])
  
  const dialogVisible = ref(false)
  
  const READ = 1
  
  const FORWARD = 2
  
  const DELETE = 3
  
  const MOVE = 4
  
  const ActionName = {
    1: lang.mark_read,
    2: lang.forward,
    3: lang.delete,
    4: lang.move
  }
  
  const init = function () {
    http.post("/api/v1/rule/get").then((res) => {
      data.value = res.data || []
    })
  }
  
  init()
  
  const groupData = reactive({
    list: []
  })
  
  const reflushGroupInfos = function () {
    http.get('/api/v1/group/list').then(res => {
      if (res.data != null) {
        groupData.list = res.data
        for (let i = 0; i < groupData.list.length; i++) {
          groupData.list[i].id += ""
        }
      }
    })
  }
  
  reflushGroupInfos()
  
  const newRuleForm = () => ({
    "id": 0,
    "name": "",
    "sort": 0,
    "rules": [{"field": "", "type": "", "rule": ""}],
    "action": "",
    "params": ""
  })
  const addRuleForm = reactive(newRuleForm())
  const createRule = () => {
    Object.assign(addRuleForm, newRuleForm())
    dialogVisible.value = true
  }
  const cancelRule = () => {
    dialogVisible.value = false
    Object.assign(addRuleForm, newRuleForm())
  }
  
  const delRule = function (id) {
    http.post("/api/v1/rule/del", {"id": id}).then((res) => {
      ElNotification({
        title: res.errorNo === 0 ? lang.succ : lang.fail,
        message: res.data,
        type: res.errorNo === 0 ? 'success' : 'error',
      })
      init()
    })
  }
  
  const editRule = function (ruleInfo) {
    addRuleForm.id = ruleInfo.id
    addRuleForm.name = ruleInfo.name
    addRuleForm.rules = (ruleInfo.rules || []).map(rule => ({ ...rule }))
    addRuleForm.action = ruleInfo.action
    addRuleForm.params = ruleInfo.params
    addRuleForm.sort = ruleInfo.sort
    dialogVisible.value = true
  }
  
  const removeRuleLine = function (index) {
    addRuleForm.rules.splice(index, 1);
  }
  
  const addRule = function () {
    addRuleForm.rules.push({"field": "", "type": "", "rule": ""})
  }
  
  const submitRule = function () {
    let api = "/api/v1/rule/add"
    if (addRuleForm.id > 0) {
      api = "/api/v1/rule/update"
    }
  
    addRuleForm.sort = parseInt(addRuleForm.sort)
  
    http.post(api, addRuleForm).then((res) => {
      if (res.errorNo !== 0) {
        ElNotification({
          title: lang.fail,
          message: res.data,
          type: 'error',
        })
      } else {
        init()
        cancelRule()
      }
    })
  }
  
  const ruleTypeChange = function () {
    addRuleForm.params = ''
  }

  return {
    reactive,
    ref,
    lang,
    Delete,
    Edit,
    InfoFilled,
    Plus,
    http,
    ElNotification,
    data,
    dialogVisible,
    READ,
    FORWARD,
    DELETE,
    MOVE,
    ActionName,
    init,
    groupData,
    reflushGroupInfos,
    addRuleForm,
    delRule,
    editRule,
    createRule,
    cancelRule,
    removeRuleLine,
    addRule,
    submitRule,
    ruleTypeChange,
    canReadGlobalPolicy
  }
}

export {
  Plus
}
