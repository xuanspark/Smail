<template>
  <section class="totp-settings" v-loading="busy">
    <h3>手机动态码（TOTP）</h3>
    <p>所有账号都可以自行选择是否开启。开启后，输入账号密码即可选择手机动态码或邮箱验证码登录。</p>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-button v-if="!ready" :disabled="busy" @click="refresh">重新读取状态</el-button>
    <template v-else>
      <el-tag :type="enabled ? 'success' : 'info'">{{ enabled ? '已启用' : '未启用' }}</el-tag>
      <div v-if="enrollment" class="totp-enrollment">
        <p>使用手机验证器扫描二维码，或手动输入下面的密钥。绑定申请十分钟内有效。</p>
        <img v-if="qrCode" :src="qrCode" alt="手机验证器绑定二维码" width="240" height="240" />
        <label>手动绑定密钥</label>
        <code>{{ enrollment.secret }}</code>
        <p>算法：SHA256；位数：6；周期：30 秒。请妥善保管密钥，勿分享给他人。</p>
        <el-form @submit.prevent="perform('confirm')">
          <el-form-item label="六位手机动态码">
            <el-input v-model="code" maxlength="6" inputmode="numeric" autocomplete="one-time-code" placeholder="输入手机上当前显示的动态码" />
          </el-form-item>
          <div class="totp-actions">
            <el-button type="primary" :disabled="busy" @click="perform('confirm')">确认启用</el-button>
            <el-button :disabled="busy" @click="perform('cancel')">取消绑定</el-button>
          </div>
        </el-form>
      </div>
      <el-form v-else-if="enabled" class="totp-disable" @submit.prevent="perform('disable')">
        <p>关闭前请输入当前动态码。刚绑定或已用于验证的动态码需要等待下一次更新。</p>
        <el-form-item label="六位手机动态码">
          <el-input v-model="code" maxlength="6" inputmode="numeric" autocomplete="one-time-code" placeholder="输入动态码以确认关闭" />
        </el-form-item>
        <el-button type="danger" plain :disabled="busy" @click="perform('disable')">关闭手机动态码</el-button>
      </el-form>
      <div v-else class="totp-actions">
        <el-button type="primary" :disabled="busy" @click="perform('begin')">绑定手机验证器</el-button>
      </div>
    </template>
  </section>
</template>

<script setup>
import { useTOTPSettings } from './TOTPSettings.js'

const { enabled, ready, busy, enrollment, qrCode, code, errorMessage, perform, refresh } = useTOTPSettings()
</script>

<style scoped src="./TOTPSettings.css"></style>
