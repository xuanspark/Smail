<template>
  <div class="third-party-authorization" v-loading="loading">
    <div class="settings-card">
      <div class="settings-header">
        <h3>已配置客户端</h3>
        <div class="group-toolbar">
          <el-button type="primary" :disabled="loading || createLoading" @click="openCreateDialog">
            <el-icon><Plus /></el-icon> 新增客户端
          </el-button>
          <el-button :loading="loading" @click="loadClientCredentials">
            <el-icon><Refresh /></el-icon> 刷新列表
          </el-button>
        </div>
      </div>

      <!-- 已配置客户端列表表格 -->
      <section class="account-group-panel client-list-panel">
        <header class="account-group-header">
          <h4>客户端授权管理</h4>
          <span class="client-count-tag" v-if="clientList && clientList.length > 0">
            共 {{ clientList.length }} 个设备
          </span>
        </header>

        <div class="table-container">
          <el-table
            :data="pagedClients"
            class="modern-table"
            style="width: 100%"
            empty-text="暂无已配置的客户端设备，点击上方「新增客户端」创建"
          >
            <el-table-column label="ID" prop="id" min-width="40" align="center" />
            <el-table-column label="客户端名称" prop="client_name" min-width="90" show-overflow-tooltip>
              <template #default="scope">
                <span class="client-name-text">{{ scope.row.client_name }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" min-width="60" align="center">
              <template #default>
                <el-tag size="small" type="success" effect="light" class="status-tag">已就绪</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="创建时间" min-width="90" align="center">
              <template #default="scope">
                <span class="time-col">{{ formatTime(scope.row.created_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="最近活跃" min-width="90" align="center">
              <template #default="scope">
                <span class="time-col">{{ formatTime(scope.row.last_used_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" min-width="160" align="right">
              <template #default="scope">
                <div class="table-op-cell">
                  <template v-if="codesRevealed && revealedCodes[scope.row.id]">
                    <code class="mini-revealed-code">{{ revealedCodes[scope.row.id] }}</code>
                    <el-button
                      type="primary"
                      size="small"
                      class="action-btn"
                      @click="copyCode(revealedCodes[scope.row.id])"
                    >
                      复制
                    </el-button>
                  </template>
                  <el-button
                    v-else
                    type="primary"
                    plain
                    size="small"
                    class="action-btn"
                    @click="openVerifyDialog(scope.row)"
                  >
                    安全验证查看明文
                  </el-button>
                  <el-button
                    type="warning"
                    plain
                    size="small"
                    class="action-btn"
                    @click="regenerateClientCode(scope.row)"
                  >
                    重置
                  </el-button>
                  <el-button
                    type="danger"
                    plain
                    size="small"
                    class="action-btn"
                    @click="revokeClientCode(scope.row)"
                  >
                    撤销删除
                  </el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 倒计时提示栏（当有授权码明文在展示时呈现） -->
        <div class="reveal-alert-bar" v-if="codesRevealed">
          <el-tag type="warning" effect="plain" class="reveal-countdown-badge">
            授权码正在安全展示，{{ revealCountdown }} 秒后自动隐藏
          </el-tag>
        </div>

        <!-- 分页控件 -->
        <div class="pagination-wrapper" v-if="clientList && clientList.length > pageSize">
          <el-pagination
            background
            layout="prev, pager, next"
            :total="clientList.length"
            :page-size="pageSize"
            :current-page="currentPage"
            @current-change="handlePageChange"
          />
        </div>
      </section>

      <!-- 通用客户端配置参数卡片 -->
      <section class="proto-guide-section">
        <div class="account-group-panel proto-panel">
          <header class="account-group-header">
            <h4>通用客户端配置参数</h4>
            <span class="proto-desc-hint">请使用完整邮箱地址登录，密码统一使用上方专属授权码</span>
          </header>

          <div class="proto-grid-body">
            <!-- IMAP 收信 -->
            <div class="proto-card">
              <div class="proto-header">
                <div class="proto-name-badge">
                  <div class="proto-title">
                    <span class="proto-name">IMAP</span>
                    <span class="proto-service">收信服务</span>
                  </div>
                  <el-tag size="small" type="primary" effect="light">SSL / TLS</el-tag>
                </div>
                <el-button
                  type="primary"
                  link
                  size="small"
                  class="btn-card-copy"
                  :disabled="!currentDomain"
                  @click="copyText(`imap.${currentDomain}:993`, 'IMAP 参数')"
                >
                  复制
                </el-button>
              </div>

              <div class="proto-fields">
                <div class="field-item">
                  <span class="field-k">服务器</span>
                  <span class="field-v font-mono">{{ currentDomain ? `imap.${currentDomain}` : '未配置邮件域名' }}</span>
                </div>
                <div class="field-item">
                  <span class="field-k">端口</span>
                  <span class="field-v font-mono highlight">993</span>
                </div>
                <div class="field-item">
                  <span class="field-k">加密</span>
                  <span class="field-v">SSL / TLS</span>
                </div>
              </div>
            </div>

            <!-- POP3 收信 -->
            <div class="proto-card">
              <div class="proto-header">
                <div class="proto-name-badge">
                  <div class="proto-title">
                    <span class="proto-name">POP3</span>
                    <span class="proto-service">收信服务</span>
                  </div>
                  <el-tag size="small" type="primary" effect="light">SSL / TLS</el-tag>
                </div>
                <el-button
                  type="primary"
                  link
                  size="small"
                  class="btn-card-copy"
                  :disabled="!currentDomain"
                  @click="copyText(`pop.${currentDomain}:995`, 'POP3 参数')"
                >
                  复制
                </el-button>
              </div>

              <div class="proto-fields">
                <div class="field-item">
                  <span class="field-k">服务器</span>
                  <span class="field-v font-mono">{{ currentDomain ? `pop.${currentDomain}` : '未配置邮件域名' }}</span>
                </div>
                <div class="field-item">
                  <span class="field-k">端口</span>
                  <span class="field-v font-mono highlight">995</span>
                </div>
                <div class="field-item">
                  <span class="field-k">加密</span>
                  <span class="field-v">SSL / TLS</span>
                </div>
              </div>
            </div>

            <!-- SMTP 发信 -->
            <div class="proto-card">
              <div class="proto-header">
                <div class="proto-name-badge">
                  <div class="proto-title">
                    <span class="proto-name">SMTP</span>
                    <span class="proto-service">发信服务</span>
                  </div>
                  <el-tag size="small" type="primary" effect="light">SSL / STARTTLS</el-tag>
                </div>
                <el-button
                  type="primary"
                  link
                  size="small"
                  class="btn-card-copy"
                  :disabled="!currentDomain"
                  @click="copyText(`smtp.${currentDomain}:465`, 'SMTP 参数')"
                >
                  复制
                </el-button>
              </div>

              <div class="proto-fields">
                <div class="field-item">
                  <span class="field-k">服务器</span>
                  <span class="field-v font-mono">{{ currentDomain ? `smtp.${currentDomain}` : '未配置邮件域名' }}</span>
                </div>
                <div class="field-item">
                  <span class="field-k">端口</span>
                  <span class="field-v font-mono highlight">465 (SSL) / 587</span>
                </div>
                <div class="field-item">
                  <span class="field-k">加密</span>
                  <span class="field-v">SSL / STARTTLS</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>

    <!-- 新增客户端弹窗 -->
    <el-dialog
      v-model="createDialogVisible"
      title="新增客户端专属授权码"
      width="min(440px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!createLoading"
      :show-close="!createLoading"
      class="compact-dialog"
      @close="closeCreateDialog"
    >
      <div class="dialog-body">
        <p class="dialog-tip">
          为您的设备（例如“iPhone 自带邮件”、“公司 Foxmail”、“Mac Mail”）创建独立授权码：
        </p>
        <el-form
          id="client-credential-create-form"
          @submit.prevent="submitCreateClient"
        >
          <el-form-item label="客户端名称">
            <el-input
              v-model="newClientName"
              placeholder="例如：iPhone 邮件、Foxmail 办公本"
              maxlength="32"
              show-word-limit
              autocomplete="off"
            />
          </el-form-item>
          <el-form-item label="登录密码">
            <el-input
              v-model="createPassword"
              type="password"
              show-password
              placeholder="验证本人密码后生成授权码"
              autocomplete="current-password"
              :disabled="createLoading"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button size="default" :disabled="createLoading" @click="closeCreateDialog">取消</el-button>
          <el-button
            type="primary"
            size="default"
            class="btn-primary-action"
            :loading="createLoading"
            :disabled="!newClientName.trim() || !createPassword || createLoading"
            native-type="submit"
            form="client-credential-create-form"
          >
            验证并生成
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
      :close-on-press-escape="!verifyLoading"
      :show-close="!verifyLoading"
      class="compact-dialog"
      @close="closeVerifyDialog"
    >
      <div class="dialog-body">
        <p class="dialog-tip">
          {{ verifyDescription }}
        </p>
        <el-form @submit.prevent="submitVerifyAndReveal">
          <el-form-item label="登录密码">
            <el-input
              v-model="verifyPassword"
              type="password"
              show-password
              placeholder="请输入当前账号登录密码"
              autocomplete="current-password"
              :disabled="verifyLoading"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button size="default" :disabled="verifyLoading" @click="closeVerifyDialog">取消</el-button>
          <el-button
            type="primary"
            size="default"
            class="btn-primary-action"
            :loading="verifyLoading"
            :disabled="!verifyPassword || verifyLoading"
            @click="submitVerifyAndReveal"
          >
            验证并{{ verifyActionLabel }}
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { usePage } from './ThirdPartyAuthorization.js'
import { Plus, Refresh } from '@element-plus/icons-vue'

const {
  loading,
  clientList,
  pagedClients,
  currentPage,
  pageSize,
  handlePageChange,
  currentDomain,
  revealedCodes,
  codesRevealed,
  revealCountdown,
  createDialogVisible,
  newClientName,
  createPassword,
  createLoading,
  verifyDialogVisible,
  verifyPassword,
  verifyLoading,
  verifyActionLabel,
  verifyDescription,
  loadClientCredentials,
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