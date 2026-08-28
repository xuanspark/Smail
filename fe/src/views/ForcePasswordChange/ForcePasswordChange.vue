<template>
  <AuthLayout
    brand-caption="账户安全"
    intro-title="首次登录安全设置"
    intro-description="当前账号使用的是临时密码。完成密码修改前，其他业务功能将保持锁定。"
  >
    <section class="force-password-content">
      <div class="force-password-card">
        <div class="force-password-heading">
          <span class="force-password-eyebrow">必须完成</span>
          <h2>修改临时密码</h2>
          <p>请设置一个仅由您本人掌握的新密码，修改成功后需要重新登录。</p>
        </div>

        <el-alert
          title="为了保护账号安全，修改密码前不能访问邮件、设置和管理功能。"
          type="warning"
          :closable="false"
          show-icon
          class="force-password-alert"
        />

        <el-form
          class="force-password-form"
          label-position="top"
          @submit.prevent="submit"
        >
          <el-form-item label="当前临时密码">
            <el-input
              v-model="form.currentPassword"
              type="password"
              autocomplete="current-password"
              placeholder="请输入当前临时密码"
              show-password
            />
          </el-form-item>

          <el-form-item label="新密码">
            <el-input
              v-model="form.newPassword"
              type="password"
              autocomplete="new-password"
              placeholder="请输入新的登录密码"
              show-password
            />
          </el-form-item>

          <el-form-item label="确认新密码">
            <el-input
              v-model="form.confirmPassword"
              type="password"
              autocomplete="new-password"
              placeholder="请再次输入新的登录密码"
              show-password
            />
          </el-form-item>

          <el-button
            type="primary"
            native-type="submit"
            size="large"
            class="force-password-submit"
            :loading="loading"
          >
            修改密码并重新登录
          </el-button>

          <el-button
            link
            type="primary"
            class="force-password-logout"
            :disabled="loading"
            @click="logout"
          >
            退出当前账号
          </el-button>
        </el-form>
      </div>
    </section>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from "@/views/components/AuthLayout/AuthLayout.vue"
import { usePage } from "./ForcePasswordChange.js"

const {
  form,
  loading,
  submit,
  logout
} = usePage()
</script>

<style scoped src="./ForcePasswordChange.css"></style>
