import {reactive, ref} from 'vue'

import lang from '../../../i18n/i18n';

import {http} from "@/utils/axios";

import {ElNotification} from "element-plus";

import {Plus} from "@element-plus/icons-vue";

export function usePage() {
  const userList = reactive([])
  
  const currentPage = ref(1)
  
  const totalPage = ref(1)
  
  const userInfoDialog = ref(false)
  
  const editModel = ref("edit")
  
  const editUserInfo = reactive({
    "account": "",
    "name": "",
    "password": "",
    "disabled": false
  })
  
  const title = ref(lang.editUser)
  
  const reflushList = function () {
    http.post('/api/v1/user/list', {"current_page": currentPage.value, "page_size": 10}).then(res => {
      userList.length = 0
      totalPage.value = res.data.total_page
      if (res.data["list"]) {
        userList.push(...res.data["list"])
      }
    })
  }
  
  const handleEdit = function (idx, row) {
    editUserInfo.account = row.Account
    editUserInfo.name = row.Name
    editUserInfo.disabled = row.Disabled === 1
    editUserInfo.password = ""
    editModel.value = "edit"
    title.value = lang.editUser
    userInfoDialog.value = true
  }
  
  const createUser = function () {
    editUserInfo.account = ""
    editUserInfo.name = ""
    editUserInfo.disabled = false
    editUserInfo.password = ""
    editModel.value = "create"
    title.value = lang.newUser
    userInfoDialog.value = true
  }
  
  const submit = function () {
    if (editModel.value === 'edit') {
      let newData = {
        "account": editUserInfo.account,
        "username": editUserInfo.name,
        "disabled": editUserInfo.disabled ? 1 : 0
      }
      if (editUserInfo.password !== "") {
        newData["password"] = editUserInfo.password
      }
  
      http.post('/api/v1/user/edit', newData).then(res => {
        ElNotification({
          title: res.errorNo === 0 ? lang.succ : lang.fail,
          message: res.errorNo === 0 ? "" : res.data,
          type: res.errorNo === 0 ? 'success' : 'error',
        })
        if (res.errorNo === 0) {
          reflushList()
          userInfoDialog.value = false
        }
      })
    } else {
      let newData = {
        "account": editUserInfo.account,
        "username": editUserInfo.name,
        "disabled": editUserInfo.disabled ? 1 : 0,
        "password": editUserInfo.password
      }
  
      http.post('/api/v1/user/create', newData).then(res => {
        ElNotification({
          title: res.errorNo === 0 ? lang.succ : lang.fail,
          message: res.errorNo === 0 ? "" : res.data,
          type: res.errorNo === 0 ? 'success' : 'error',
        })
        if (res.errorNo === 0) {
          reflushList()
          userInfoDialog.value = false
        }
      })
    }
  }
  
  reflushList()

  return {
    reactive,
    ref,
    lang,
    http,
    ElNotification,
    Plus,
    userList,
    currentPage,
    totalPage,
    userInfoDialog,
    editModel,
    editUserInfo,
    title,
    reflushList,
    handleEdit,
    createUser,
    submit
  }
}

export {
  Plus
}
