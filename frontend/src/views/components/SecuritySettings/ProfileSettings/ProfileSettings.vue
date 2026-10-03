<template>
  <section class="security-section profile-settings">
    <div class="settings-header">
      <h3>个人资料</h3>
    </div>
    <el-form label-position="top" @submit.prevent="save">
      <el-form-item label="当前账号">
        <el-input :model-value="store.userInfos.account" disabled />
      </el-form-item>
      <el-form-item label="权限级别">
        <el-tag :type="roleMeta(store.role).type">{{ roleMeta(store.role).label }}</el-tag>
      </el-form-item>
      <el-form-item label="显示名称">
        <el-input v-model="name" placeholder="请输入 1–10 个字符" />
      </el-form-item>
      <el-button type="primary" native-type="submit" :loading="saving">
        保存资料
      </el-button>
    </el-form>
  </section>
</template>

<script setup>
import { useProfile } from './ProfileSettings.js'
const { store, name, saving, save, roleMeta } = useProfile()
</script>

<style scoped src="./ProfileSettings.css"></style>
