<template>
  <div class="third-party-authorization" v-loading="loading">
    <section class="authorization-section">
      <!-- 顶部轻量工具条：纯净标题与说明 -->
      <div class="compact-header">
        <div class="header-left">
          <span class="header-title">客户端专属授权码</span>
          <span class="header-subtitle">为手机、Foxmail 等设备创建独立授权码，随时查看或撤销</span>
        </div>
      </div>

      <!-- 客户端选择与管理主卡片 -->
      <div class="client-main-card">
        <div class="client-picker-row">
          <div class="picker-group">
            <span class="picker-label">选择客户端：</span>
            <el-select
              v-model="selectedClientId"
              placeholder="请选择或添加客户端"
              size="default"
              class="client-select"
              @change="handleClientSelect"
            >
              <el-option
                v-for="item in clientList"
                :key="item.id"
                :label="item.client_name"
                :value="item.id"
              >
                <div class="client-option-item">
                  <span>{{ item.client_name }}</span>
                  <el-tag size="small" type="success">已就绪</el-tag>
                </div>
              </el-option>
            </el-select>

            <el-button
              type="primary"
              size="default"
              class="btn-create-client"
              @click="openCreateDialog"
            >
              + 新增客户端
            </el-button>
          </div>

          <div class="picker-actions" v-if="currentClient">
            <el-button
              type="warning"
              plain
              size="default"
              @click="regenerateClientCode(currentClient)"
            >
              重新生成
            </el-button>
            <el-button
              type="danger"
              plain
              size="default"
              @click="revokeClientCode(currentClient)"
            >
              撤销删除
            </el-button>
          </div>
        </div>

        <!-- 当前选中客户端的授权码展示条 -->
        <div class="client-detail-box" v-if="currentClient">
          <div class="code-display-row">
            <span class="code-label">当前专属授权码：</span>
            <div class="code-value-area">
              <template v-if="codesRevealed && revealedCodes[currentClient.id]">
                <code class="revealed-code">{{ revealedCodes[currentClient.id] }}</code>
                <el-button
                  type="primary"
                  size="default"
                  class="action-btn"
                  @click="copyCode(revealedCodes[currentClient.id])"
                >
                  一键复制
                </el-button>
                <el-tag
                  size="default"
                  type="warning"
                  effect="plain"
                  class="countdown-tag"
                >
                  {{ revealCountdown }}s 后自动隐藏
                </el-tag>
              </template>
              <template v-else-if="codesRevealed && !revealedCodes[currentClient.id]">
                <span class="text-tip">未保留加密密文，可点击重新生成</span>
              </template>
              <template v-else>
                <span class="masked-code">••••••••••••••••••••</span>
                <el-button
                  type="primary"
                  plain
                  size="default"
                  class="btn-verify-reveal"
                  @click="openVerifyDialog"
                >
                  安全验证查看明文
                </el-button>
              </template>
            </div>
            <div class="client-meta">
              <span class="meta-item">创建时间：{{ formatTime(currentClient.created_at) }}</span>
              <span class="meta-item" v-if="currentClient.last_used_at">
                最近活跃：{{ formatTime(currentClient.last_used_at) }}
              </span>
            </div>
          </div>
        </div>

        <div class="empty-client-box" v-else>
          <span class="empty-tip">暂无可用客户端，请点击上方「+ 新增客户端」为您的设备配置专属授权码</span>
          <div class="empty-action">
            <el-button
              type="primary"
              size="default"
              @click="openCreateDialog"
            >
              + 立即新增客户端
            </el-button>
          </div>
        </div>
      </div>

      <!-- 客户端配置指引紧凑网格（一屏显示完整，无滚动条） -->
      <div class="compact-guide-section">
        <div class="guide-title">
          <span>通用客户端配置参数</span>
          <span class="guide-sub">支持用户名直接登录或完整邮箱登录，密码统一使用上方专属授权码</span>
        </div>

        <div class="guide-grid">
          <div class="guide-card">
            <div class="card-head">
              <span class="card-proto">IMAP 收信服务</span>
              <el-tag size="small" type="primary">SSL/TLS</el-tag>
            </div>
            <div class="card-line">
              <span class="cl-label">服务器地址：</span>
              <span class="cl-val">imap.{{ currentDomain || 'solit.cn' }}</span>
            </div>
            <div class="card-line">
              <span class="cl-label">服务端口：</span>
              <span class="cl-val highlight">993</span>
            </div>
          </div>

          <div class="guide-card">
            <div class="card-head">
              <span class="card-proto">POP3 收信服务</span>
              <el-tag size="small" type="primary">SSL/TLS</el-tag>
            </div>
            <div class="card-line">
              <span class="cl-label">服务器地址：</span>
              <span class="cl-val">pop.{{ currentDomain || 'solit.cn' }}</span>
            </div>
            <div class="card-line">
              <span class="cl-label">服务端口：</span>
              <span class="cl-val highlight">995</span>
            </div>
          </div>

          <div class="guide-card">
            <div class="card-head">
              <span class="card-proto">SMTP 发信服务</span>
              <el-tag size="small" type="primary">SSL / STARTTLS</el-tag>
            </div>
            <div class="card-line">
              <span class="cl-label">服务器地址：</span>
              <span class="cl-val">smtp.{{ currentDomain || 'solit.cn' }}</span>
            </div>
            <div class="card-line">
              <span class="cl-label">服务端口：</span>
              <span class="cl-val highlight">465 (SSL) / 587</span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 新增客户端弹窗 -->
    <el-dialog
      v-model="createDialogVisible"
      title="新增客户端专属授权码"
      width="min(440px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      class="compact-dialog"
      @close="closeCreateDialog"
    >
      <div class="dialog-body">
        <p class="dialog-tip">
          为您的设备（例如“iPhone 自带邮件”、“公司 Foxmail”、“Mac Mail”）创建独立授权码：
        </p>
        <el-form @submit.prevent="submitCreateClient">
          <el-form-item label="客户端名称">
            <el-input
              v-model="newClientName"
              placeholder="例如：iPhone 邮件、Foxmail 办公本"
              maxlength="32"
              show-word-limit
              autocomplete="off"
              @keyup.enter="submitCreateClient"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button size="default" @click="closeCreateDialog">取消</el-button>
          <el-button
            type="primary"
            size="default"
            :loading="createLoading"
            :disabled="!newClientName.trim()"
            @click="submitCreateClient"
          >
            立即生成
          </el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 密码验证弹窗 -->
    <el-dialog
      v-model="verifyDialogVisible"
      title="安全身份验证"
      width="min(440px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      class="compact-dialog"
      @close="closeVerifyDialog"
    >
      <div class="dialog-body">
        <p class="dialog-tip">
          查看已保存的客户端专属授权码明文前，请先验证当前账号的登录密码：
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
        <div class="dialog-footer">
          <el-button size="default" @click="closeVerifyDialog">取消</el-button>
          <el-button
            type="primary"
            size="default"
            :loading="verifyLoading"
            :disabled="!verifyPassword.trim()"
            @click="submitVerifyAndReveal"
          >
            验证并查看明文
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
  loading,
  clientList,
  selectedClientId,
  currentClient,
  currentDomain,
  revealedCodes,
  codesRevealed,
  revealCountdown,
  createDialogVisible,
  newClientName,
  createLoading,
  verifyDialogVisible,
  verifyPassword,
  verifyLoading,
  handleClientSelect,
  openCreateDialog,
  closeCreateDialog,
  submitCreateClient,
  regenerateClientCode,
  revokeClientCode,
  copyCode,
  formatTime,
  openVerifyDialog,
  closeVerifyDialog,
  submitVerifyAndReveal,
} = usePage()

const hasClients = computed(() =>
  Array.isArray(clientList.value) && clientList.value.length > 0
)
</script>

<style scoped src="./ThirdPartyAuthorization.css"></style>