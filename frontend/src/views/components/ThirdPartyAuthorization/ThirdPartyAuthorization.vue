<template>
  <div class="third-party-authorization" v-loading="protocolLoading">
    <section class="authorization-section">
      <div class="authorization-header">
        <h3>邮件客户端协议</h3>
        <p class="authorization-desc">
          本地邮件服务默认启动，不依赖 SMTP 中继。以下开关只控制当前账号的客户端访问权限，不影响其他用户或服务监听。
        </p>
      </div>

      <el-alert
        title="登录名填写完整邮箱地址，密码填写本协议授权码，不能使用网页登录密码。授权码仅显示一次；关闭协议会暂停新登录，重新开启可继续使用未撤销的授权码。"
        type="warning"
        :closable="false"
        show-icon
      />

      <div class="protocol-list">
        <div v-for="item in protocolCredentials" :key="item.protocol" class="protocol-item">
          <div class="protocol-info">
            <div class="protocol-title">
              <strong>{{ protocolLabel(item.protocol) }}</strong>
              <el-tag size="small" :type="item.enabled ? 'success' : 'info'">
                {{ item.enabled ? '已开启' : '已关闭' }}
              </el-tag>
              <el-tag size="small" :type="item.configured ? 'success' : 'warning'">
                {{ item.configured ? '已配置授权码' : '未生成授权码' }}
              </el-tag>
            </div>
            <p>登录名：完整邮箱地址；密码：本协议授权码。</p>
          </div>

          <div class="protocol-actions">
            <el-switch
              :model-value="item.enabled"
              :loading="protocolLoading"
              active-text="开启"
              inactive-text="关闭"
              :aria-label="`${protocolLabel(item.protocol)} 当前账号客户端访问`"
              @change="value => setProtocolEnabled(item, value)"
            />
            <el-button
              type="primary"
              size="small"
              :disabled="!item.enabled"
              @click="generateProtocolCode(item)"
            >
              {{ item.configured ? '重新生成授权码' : '生成授权码' }}
            </el-button>
            <el-button
              v-if="item.configured"
              type="danger"
              plain
              size="small"
              @click="revokeProtocolCode(item)"
            >
              撤销授权码
            </el-button>
          </div>
        </div>
      </div>

      <div v-if="generatedProtocolCode" class="protocol-code-panel">
        <div class="one-time-tip">
          {{ protocolLabel(generatedProtocol) }} 授权码仅本次显示，请复制保存：
        </div>
        <code>{{ generatedProtocolCode }}</code>
        <el-button type="primary" plain size="small" @click="copyProtocolCode">
          复制授权码
        </el-button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { usePage } from './ThirdPartyAuthorization.js'

const {
  protocolLoading,
  protocolCredentials,
  generatedProtocolCode,
  generatedProtocol,
  protocolLabel,
  setProtocolEnabled,
  generateProtocolCode,
  copyProtocolCode,
  revokeProtocolCode,
} = usePage()
</script>

<style scoped src="./ThirdPartyAuthorization.css"></style>
