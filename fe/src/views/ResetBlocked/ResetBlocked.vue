<template>
<main class="reset-blocked-page" aria-labelledby="reset-blocked-title">
    <section class="reset-blocked-card">
      <div class="brand-mark" aria-hidden="true">S</div>
      <p class="eyebrow">Smail 安全边界</p>
      <h1 id="reset-blocked-title">重置入口未开放</h1>
      <p class="message">
        超级管理员重置不通过普通 Web 页面执行。请在部署主机上使用
        <code>systemctl start smail-admin</code> 启动专用重置服务。
      </p>
      <p class="hint">当前页面不会读取或初始化普通用户登录会话。</p>
      <button type="button" class="back-button" @click="goLogin">返回登录</button>
    </section>
  </main>
</template>

<script setup>
import { usePage } from './ResetBlocked.js'

const {
  goLogin
} = usePage()
</script>

<style scoped src="./ResetBlocked.css"></style>
