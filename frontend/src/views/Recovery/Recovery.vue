<template>
  <AuthLayout
    brand-caption="超级管理员密码重置"
    intro-title="重置超级管理员密码"
    intro-description="超级管理员密码不支持通过网页恢复，只能由部署端本机安全通道完成。"
  >
    <template #intro-extra>
      <el-alert
        title="为避免远程接管风险，网页端不会收集或验证超级管理员重置凭据。"
        type="warning"
        :closable="false"
        show-icon
        class="reset-boundary"
      />
    </template>

    <section class="recovery-content" aria-labelledby="recovery-title">
      <div class="security-notice">
        <span class="notice-icon" aria-hidden="true">!</span>
        <div>
          <h2 id="recovery-title">请使用本机部署端工具</h2>
          <p>
            超级管理员重置必须在部署服务器本机发起，并由部署管理员进行交互式确认。
            普通登录页、忘记密码页和其他 Web API 均不能开启或完成该流程。
          </p>
        </div>
      </div>

      <div class="steps-card">
        <h3>操作步骤</h3>
        <ol>
          <li>登录部署服务器本机终端，确认当前服务版本和运行状态。</li>
          <li>通过受保护的本机管理通道启动 <code>smail-admin</code>。</li>
          <li>按终端提示完成身份确认和新密码输入，密码不会回显或写入日志。</li>
          <li>重置成功后，使用新密码重新登录，并重新配置需要的安全凭据。</li>
        </ol>
      </div>

      <div class="channel-card">
        <div class="channel-heading">
          <span>专用安全通道</span>
          <strong>smail-admin</strong>
        </div>
        <p>
          该工具只通过本机 IPC 与 Smail 主服务通信，不读取数据库密码，不监听网络端口，
          也不能执行任意命令或访问任意文件。
        </p>
        <div class="channel-hint">
          <span>Linux：Unix Domain Socket</span>
          <span>Windows：Named Pipe</span>
        </div>
      </div>

      <el-button
        type="primary"
        class="back-button"
        @click="goLogin"
      >
        返回登录
      </el-button>
    </section>
  </AuthLayout>
</template>

<script setup>
import AuthLayout from '@/views/components/AuthLayout/AuthLayout.vue'
import { usePage } from './Recovery.js'

const { goLogin } = usePage()
</script>

<style scoped src="./Recovery.css"></style>