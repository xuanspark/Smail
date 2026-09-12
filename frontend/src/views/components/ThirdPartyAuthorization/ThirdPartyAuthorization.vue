<template>
  <div class="third-party-authorization" v-loading="protocolLoading">
    <section class="authorization-section">
      <div class="authorization-header">
        <div class="header-main">
          <h3>邮件客户端协议</h3>
          <p class="authorization-desc">
            为 IMAP、POP3、SMTP 配置专属授权码，平时安全隐藏，验证密码后可查看或复制。
          </p>
        </div>
        <div class="header-actions">
          <el-button
            v-if="hasConfiguredProtocols && !codesRevealed"
            type="primary"
            plain
            size="small"
            @click="openVerifyDialog"
          >
            安全验证查看授权码
          </el-button>
          <el-button
            v-else-if="codesRevealed"
            type="info"
            plain
            size="small"
            @click="hideRevealedCodes"
          >
            安全隐藏授权码
          </el-button>
        </div>
      </div>

      <el-alert
        title="使用完整邮箱地址作为登录名，专属授权码作为密码。授权码平时处于加密保护状态，验证密码后可完整查看与一键复制。"
        type="info"
        :closable="false"
        show-icon
      />

      <div class="protocol-list">
        <div
          v-for="item in protocolCredentials"
          :key="item.protocol"
          class="protocol-item"
        >
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
            <p class="protocol-guide">登录名：完整邮箱地址；密码：本协议专属授权码。</p>

            <div class="protocol-credential-slot">
              <span class="slot-label">当前授权码：</span>
              <template v-if="item.configured">
                <template v-if="codesRevealed && revealedCodes[item.protocol]">
                  <code class="revealed-code">{{ revealedCodes[item.protocol] }}</code>
                  <el-button
                    type="primary"
                    link
                    size="small"
                    class="copy-btn"
                    @click="copyProtocolCode(revealedCodes[item.protocol])"
                  >
                    复制
                  </el-button>
                </template>
                <template v-else>
                  <span class="masked-code">••••••••••••••••</span>
                  <el-button
                    type="primary"
                    link
                    size="small"
                    class="reveal-btn"
                    @click="openVerifyDialog"
                  >
                    验证查看
                  </el-button>
                </template>
              </template>
              <template v-else>
                <span class="unconfigured-tip">尚未生成专属授权码，请点击右侧生成</span>
              </template>
            </div>
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
    </section>

    <!-- 密码验证弹窗 -->
    <el-dialog
      v-model="verifyDialogVisible"
      title="安全身份验证"
      width="min(440px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      class="verify-reveal-dialog"
      @close="closeVerifyDialog"
    >
      <div class="verify-dialog-body">
        <p class="verify-dialog-tip">
          查看已持久保存的协议授权码前，请先验证当前账号的登录密码：
        </p>
        <el-form @submit.prevent="submitVerifyAndReveal">
          <el-form-item label="登录密码">
            <el-input
              v-model="verifyPassword"
              type="password"
              show-password
              placeholder="请输入当前账号登录密码"
              autocomplete="current-password"
              @keyup.enter="submitVerifyAndReveal"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="verify-dialog-footer">
          <el-button @click="closeVerifyDialog">取消</el-button>
          <el-button
            type="primary"
            :loading="verifyLoading"
            :disabled="!verifyPassword.trim()"
            @click="submitVerifyAndReveal"
          >
            验证并查看授权码
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { usePage } from './ThirdPartyAuthorization.js'

const {
  protocolLoading,
  protocolCredentials,
  revealedCodes,
  codesRevealed,
  verifyDialogVisible,
  verifyPassword,
  verifyLoading,
  protocolLabel,
  setProtocolEnabled,
  generateProtocolCode,
  copyProtocolCode,
  revokeProtocolCode,
  openVerifyDialog,
  closeVerifyDialog,
  submitVerifyAndReveal,
  hideRevealedCodes,
} = usePage()

const hasConfiguredProtocols = computed(() =>
  Array.isArray(protocolCredentials.value) &&
  protocolCredentials.value.some(item => item.configured)
)
</script>

<style scoped src="./ThirdPartyAuthorization.css"></style>