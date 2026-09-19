<template>
  <div class="third-party-authorization" v-loading="loading">
    <section class="authorization-section">
      <!-- 客户端与专属授权码管理卡片 -->
      <div class="panel-card client-control-card">
        <div class="card-header-bar">
          <div class="header-left">
            <span class="main-title">客户端专属授权码</span>
            <span class="sub-title">为手机、Foxmail、Outlook 等设备创建独立授权码，随时查看或撤销</span>
          </div>

          <div class="header-actions">
            <el-button
              type="primary"
              size="default"
              class="btn-primary-action"
              @click="openCreateDialog"
            >
              + 新增客户端
            </el-button>
          </div>
        </div>

        <div class="card-content-area">
          <div class="client-op-row">
            <div class="op-left-group">
              <span class="item-label">选择客户端：</span>
              <el-select
                v-model="selectedClientId"
                placeholder="请选择客户端设备"
                size="default"
                class="client-select-input"
                @change="handleClientSelect"
              >
                <el-option
                  v-for="item in clientList"
                  :key="item.id"
                  :label="item.client_name"
                  :value="item.id"
                >
                  <div class="client-option-item">
                    <span class="opt-name">{{ item.client_name }}</span>
                    <el-tag size="small" type="success" effect="light">已就绪</el-tag>
                  </div>
                </el-option>
              </el-select>
            </div>

            <div class="op-right-group" v-if="currentClient">
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

          <!-- 授权码展示横条 -->
          <div class="code-banner" v-if="currentClient">
            <div class="banner-left">
              <span class="banner-label">当前专属授权码：</span>
              <div class="banner-code-box">
                <template v-if="codesRevealed && revealedCodes[currentClient.id]">
                  <code class="revealed-code">{{ revealedCodes[currentClient.id] }}</code>
                  <el-button
                    type="primary"
                    size="default"
                    class="btn-copy-code"
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
                  <span class="text-hint">未保留加密密文，可点击右上方重新生成</span>
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
            </div>

            <div class="banner-right">
              <span class="meta-tag">创建时间：{{ formatTime(currentClient.created_at) }}</span>
              <span class="meta-tag" v-if="currentClient.last_used_at">
                最近活跃：{{ formatTime(currentClient.last_used_at) }}
              </span>
            </div>
          </div>

          <!-- 空状态引导 -->
          <div class="empty-client-placeholder" v-else>
            <div class="placeholder-text">暂无可用客户端，请点击右上方「+ 新增客户端」为您的设备配置专属授权码</div>
            <el-button
              type="primary"
              size="default"
              class="btn-primary-action"
              @click="openCreateDialog"
            >
              + 立即新增客户端
            </el-button>
          </div>
        </div>
      </div>

      <!-- 通用客户端配置参数卡片 -->
      <div class="panel-card proto-guide-card">
        <div class="card-header-bar">
          <div class="header-left">
            <span class="main-title">通用客户端配置参数</span>
            <span class="sub-title">请使用完整邮箱地址登录，密码统一使用上方专属授权码</span>
          </div>
        </div>

        <div class="proto-grid">
          <!-- IMAP 收信 -->
          <div class="proto-card">
            <div class="proto-header">
              <div class="proto-name-badge">
                <span class="proto-name">IMAP 收信服务</span>
                <el-tag size="small" type="primary" effect="light">SSL / TLS</el-tag>
              </div>
              <el-button
                type="primary"
                link
                size="small"
                class="btn-card-copy"
                @click="copyText(`imap.${currentDomain || 'solit.cn'}:993`, 'IMAP 参数')"
              >
                复制
              </el-button>
            </div>

            <div class="proto-fields">
              <div class="field-item">
                <span class="field-k">服务器地址：</span>
                <span class="field-v font-mono">imap.{{ currentDomain || 'solit.cn' }}</span>
              </div>
              <div class="field-item">
                <span class="field-k">服务端口：</span>
                <span class="field-v font-mono highlight">993</span>
              </div>
              <div class="field-item">
                <span class="field-k">加密方式：</span>
                <span class="field-v">SSL / TLS 隐式加密</span>
              </div>
            </div>
          </div>

          <!-- POP3 收信 -->
          <div class="proto-card">
            <div class="proto-header">
              <div class="proto-name-badge">
                <span class="proto-name">POP3 收信服务</span>
                <el-tag size="small" type="primary" effect="light">SSL / TLS</el-tag>
              </div>
              <el-button
                type="primary"
                link
                size="small"
                class="btn-card-copy"
                @click="copyText(`pop.${currentDomain || 'solit.cn'}:995`, 'POP3 参数')"
              >
                复制
              </el-button>
            </div>

            <div class="proto-fields">
              <div class="field-item">
                <span class="field-k">服务器地址：</span>
                <span class="field-v font-mono">pop.{{ currentDomain || 'solit.cn' }}</span>
              </div>
              <div class="field-item">
                <span class="field-k">服务端口：</span>
                <span class="field-v font-mono highlight">995</span>
              </div>
              <div class="field-item">
                <span class="field-k">加密方式：</span>
                <span class="field-v">SSL / TLS 隐式加密</span>
              </div>
            </div>
          </div>

          <!-- SMTP 发信 -->
          <div class="proto-card">
            <div class="proto-header">
              <div class="proto-name-badge">
                <span class="proto-name">SMTP 发信服务</span>
                <el-tag size="small" type="primary" effect="light">SSL / STARTTLS</el-tag>
              </div>
              <el-button
                type="primary"
                link
                size="small"
                class="btn-card-copy"
                @click="copyText(`smtp.${currentDomain || 'solit.cn'}:465`, 'SMTP 参数')"
              >
                复制
              </el-button>
            </div>

            <div class="proto-fields">
              <div class="field-item">
                <span class="field-k">服务器地址：</span>
                <span class="field-v font-mono">smtp.{{ currentDomain || 'solit.cn' }}</span>
              </div>
              <div class="field-item">
                <span class="field-k">服务端口：</span>
                <span class="field-v font-mono highlight">465 (SSL) / 587</span>
              </div>
              <div class="field-item">
                <span class="field-k">加密方式：</span>
                <span class="field-v">SSL 隐式加密 / STARTTLS</span>
              </div>
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
            class="btn-primary-action"
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
            class="btn-primary-action"
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
  copyText,
  copyCode,
  formatTime,
  openVerifyDialog,
  closeVerifyDialog,
  submitVerifyAndReveal,
} = usePage()

</script>

<style scoped src="./ThirdPartyAuthorization.css"></style>
