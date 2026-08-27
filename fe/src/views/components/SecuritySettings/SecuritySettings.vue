<template>
<div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.modify_pwd }}</h3>
      <p class="settings-desc">{{ lang.modify_pwd_desc }}</p>
    </div>

    <div class="settings-body">
      <el-form :model="ruleForm" :rules="rules" status-icon label-position="top" class="settings-form">
        <el-form-item :label="lang.current_pwd" prop="current_pwd">
          <el-input type="password" v-model="ruleForm.current_pwd" :placeholder="lang.enter_current_pwd" show-password class="premium-input"/>
        </el-form-item>
        <el-form-item :label="lang.modify_pwd" prop="new_pwd">
          <el-input type="password" v-model="ruleForm.new_pwd" :placeholder="lang.enter_new_pwd" show-password class="premium-input"/>
        </el-form-item>
        <el-form-item :label="lang.enter_again" prop="new_pwd2">
          <el-input type="password" v-model="ruleForm.new_pwd2" :placeholder="lang.confirm_new_pwd" show-password class="premium-input"/>
        </el-form-item>
        <div class="form-actions">
          <el-button type="primary" @click="submit" class="submit-btn">{{ lang.submit }}</el-button>
        </div>
      </el-form>
    </div>

    <el-divider class="settings-divider"/>

    <div class="settings-header security-key-header">
      <div>
        <h3>WebAuthn 安全密钥</h3>
        <p class="settings-desc">使用设备或浏览器内置的安全密钥无密码登录。</p>
      </div>
      <el-button type="primary" :loading="registering" :disabled="!supported" @click="registerCredential">
        注册安全密钥
      </el-button>
    </div>
    <el-alert
      v-if="!supported"
      title="当前浏览器不支持安全密钥，请使用 HTTPS 访问并确保浏览器支持 WebAuthn"
      type="warning"
      :closable="false"
      show-icon
      class="security-alert"
    />
    <div class="credential-list" v-loading="credentialsLoading">
      <el-empty v-if="!credentialsLoading && credentials.length === 0" description="暂无已注册的安全密钥"/>
      <div v-for="item in credentials" :key="item.id" class="credential-item">
        <div class="credential-info">
          <strong>{{ item.display_name }}</strong>
          <span>创建于{{ formatTime(item.created_at) }}</span>
          <span v-if="item.last_used_at">最后使用于{{ formatTime(item.last_used_at) }}</span>
          <el-tag v-if="item.revoked_at" type="info" size="small">已撤销</el-tag>
          <el-tag v-else-if="item.requires_re_registration" type="warning" size="small">需要重新注册</el-tag>
          <el-tag v-else type="success" size="small">有效</el-tag>
          <span v-if="item.requires_re_registration" class="credential-warning">该密钥为旧版本，请重新注册后再用于登录。</span>
        </div>
        <div class="credential-actions">
          <el-button size="small" :disabled="Boolean(item.revoked_at)" @click="renameCredential(item)">重命名</el-button>
          <el-button size="small" type="danger" plain :disabled="Boolean(item.revoked_at)" @click="revokeCredential(item)">撤销</el-button>
        </div>
      </div>
    </div>

    <el-divider class="settings-divider"/>

    <div class="settings-header">
      <h3 class="danger-text">{{ lang.logout }}</h3>
      <p class="settings-desc">{{ lang.logout_desc }}</p>
    </div>
    <div class="settings-body">
      <el-button type="danger" plain @click="logout" class="logout-btn">{{ lang.logout }}</el-button>
    </div>
  </div>
</template>

<script setup>
import { usePage } from './SecuritySettings.js'

const {
  lang,
  ruleForm,
  rules,
  submit,
  registering,
  supported,
  registerCredential,
  credentialsLoading,
  credentials,
  formatTime,
  renameCredential,
  revokeCredential,
  logout
} = usePage()
</script>

<style scoped src="./SecuritySettings.css"></style>
