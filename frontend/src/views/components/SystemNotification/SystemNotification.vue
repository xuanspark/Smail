<template>
  <section class="notification-settings" aria-label="系统通知发信">
    <header class="notification-header">
      <div v-if="!compact">
        <h2>系统通知发信</h2>
        <p>按账号授权的 SMTP 发信服务，与普通邮箱登录和收发信独立。</p>
      </div>
      <el-button :disabled="submitting || loading" @click="refresh">刷新状态</el-button>
    </header>

    <el-alert
      v-if="!compact"
      title="默认关闭，逐账号授权"
      :description="manager ? '只有管理范围内的账号可以授权。管理员修改授权前需要身份确认；关闭时永久撤销旧凭据。' : '可查看自己的状态，并验证本人密码查看通知凭据。开启或重置凭据请联系管理员。'"
      type="info"
      show-icon
      :closable="false"
    />

    <div v-if="manager && !userId" class="notification-panel notification-manager-panel">
      <h3>账号授权管理</h3>
      <el-table :data="accounts" stripe>
        <el-table-column prop="account" label="账号" min-width="110" />
        <el-table-column prop="name" label="名称" min-width="90" />
        <el-table-column label="通知状态" min-width="140">
          <template #default="{ row }">
            <el-tag :type="row.enabled && row.configured ? 'success' : 'info'">
              {{ row.disabled ? '账号已禁用' : !row.enabled ? '未开启' : row.configured ? '已开启' : '待生成凭据' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <div class="notification-row-actions">
              <el-button link type="primary" :disabled="submitting || loading" @click="openManage(row)">管理</el-button>
              <el-button
                link
                type="primary"
                :disabled="submitting || loading || !row.configured || row.disabled"
                @click="revealAccount(row)"
              >查看授权码</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page"
        :page-size="20"
        :total="total"
        layout="prev, pager, next"
        :disabled="submitting"
        @current-change="loadAccounts"
      />
      <div v-if="manager && !userId && secretVisible && secretAccount" class="notification-manager-secret" role="status">
        <div class="notification-manager-secret-heading">
          <span>账号 {{ secretAccount }} 的通知授权码</span>
          <span class="notification-countdown">{{ seconds }} 秒后自动隐藏</span>
        </div>
        <code class="notification-credential-code">{{ secret }}</code>
        <div class="notification-credential-actions">
          <el-button size="small" type="primary" @click="copySecret">复制授权码</el-button>
          <el-button size="small" @click="clearSecret">隐藏</el-button>
        </div>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div
      v-if="!manager || userId || manageVisible"
      class="notification-detail-modal notification-detail-panel"
      :class="{
        'notification-detail-modal-active': manager && !userId,
        'notification-personal-panel': !manager && !userId
      }"
      role="dialog"
      :aria-modal="manager && !userId ? 'true' : undefined"
      @click.self="manager && !userId ? closeManage() : null"
    >
      <div v-loading="loading" class="notification-panel notification-detail-dialog" aria-live="polite">
      <template v-if="status">
        <div class="notification-heading">
          <h3>账号：{{ status.account }}</h3>
          <div class="notification-heading-actions">
            <el-tag :type="status.enabled && status.configured ? 'success' : 'info'">{{ stateText }}</el-tag>
            <el-button v-if="manager && !userId" link @click="closeManage">关闭</el-button>
          </div>
        </div>
        <p v-if="!status.account_available" class="notification-warning">账号已禁用或尚未修改临时密码，不能生成或使用通知凭据。</p>

        <div v-if="status.configured && (!manager || userId)" class="notification-credential">
          <span class="notification-credential-label">通知发信凭据</span>
          <template v-if="secretVisible && secret">
            <code class="notification-credential-code">{{ secret }}</code>
            <div class="notification-credential-actions">
              <el-button size="small" type="primary" @click="copySecret">复制凭据</el-button>
              <el-button size="small" @click="clearSecret">隐藏</el-button>
              <span class="notification-countdown" role="status">{{ seconds }} 秒后自动隐藏</span>
            </div>
          </template>
          <template v-else>
            <span class="notification-credential-mask" aria-label="凭据已隐藏">••••••••••••••••</span>
            <el-button
              v-if="canReveal"
              size="small"
              type="primary"
              plain
              :disabled="submitting || loading || dirty"
              @click="openVerify"
            >安全验证查看明文</el-button>
            <span v-else class="notification-hint">旧凭据未保留密文，请联系管理员重新生成。</span>
          </template>
        </div>

        <div class="notification-columns">
          <div class="notification-control-column">
        <el-form v-if="canManage" label-position="top" :disabled="submitting || loading" @submit.prevent="save">
          <el-form-item label="允许该账号通过 SMTP 发送系统通知">
            <el-switch v-model="form.enabled" active-text="开启" inactive-text="关闭" />
          </el-form-item>
          <el-form-item label="自定义发件名称（可选）">
            <el-input
              v-model="form.sender_name"
              maxlength="30"
              placeholder="留空默认显示“系统通知”"
              clearable
            />
          </el-form-item>
          <div class="notification-limits">
            <el-form-item label="每分钟收件人限额">
              <el-input-number v-model="form.minute_limit" :min="1" :max="100" :precision="0" />
            </el-form-item>
            <el-form-item label="每日收件人限额（UTC）">
              <el-input-number v-model="form.day_limit" :min="1" :max="10000" :precision="0" />
            </el-form-item>
          </div>
          <div class="notification-actions">
            <el-button type="primary" :loading="submitting" :disabled="!dirty" native-type="submit">保存授权</el-button>
            <el-button
              :disabled="!status.enabled || !status.account_available || dirty || submitting"
              @click="rotate"
            >{{ status.configured ? '重置发信凭据' : '生成发信凭据' }}</el-button>
          </div>
        </el-form>

        <div class="notification-usage-block">
        <el-descriptions title="使用状态" :column="1" border>
          <el-descriptions-item label="本分钟用量">{{ status.minute_used }} / {{ status.minute_limit }} 个收件人</el-descriptions-item>
          <el-descriptions-item label="今日用量（UTC）">{{ status.day_used }} / {{ status.day_limit }} 个收件人</el-descriptions-item>
          <el-descriptions-item label="单封限制">最多 {{ status.max_recipients }} 个收件人，最大 {{ status.max_message_bytes / 1048576 }} MiB</el-descriptions-item>
        </el-descriptions>
        <p class="notification-hint">配额按提交的收件人数计算，失败和重试也计数；更换凭据或重启不会清空配额。</p>
        </div>
        </div>

        <div class="notification-service-column">
        <el-descriptions title="应用 SMTP 配置" :column="1" border>
          <el-descriptions-item label="服务器">{{ status.host }}</el-descriptions-item>
          <el-descriptions-item label="隐式 TLS 端口">{{ status.tls_port || '未启用' }}</el-descriptions-item>
          <el-descriptions-item label="STARTTLS 端口">{{ status.starttls_port || '未启用' }}</el-descriptions-item>
          <el-descriptions-item label="SMTP 用户名"><code>{{ status.username }}</code></el-descriptions-item>
          <el-descriptions-item label="SMTP 密码">独立发信凭据</el-descriptions-item>
          <el-descriptions-item label="发件邮箱"><code>{{ status.sender }}</code></el-descriptions-item>
          <el-descriptions-item label="发件名称">{{ status.sender_name }}（未自定义时默认“系统通知”）</el-descriptions-item>
        </el-descriptions>
        <p class="notification-hint">必须启用加密和证书校验。服务器名需正确解析且由证书覆盖；此处显示配置，不代表公网连通性已验收。</p>
        </div>
        <el-alert class="notification-send-only-alert" title="仅发信，不提供收件箱" description="通知地址拒收邮件；通知凭据不能用于网页登录、IMAP 或 POP3。普通邮箱及其原有凭据不受影响。" type="warning" :closable="false" show-icon />
        </div>
      </template>
      <el-empty v-else-if="!loading && !error" description="尚未读取账号状态" />
      </div>
    </div>

    <el-dialog
      v-model="verifyVisible"
      title="安全验证"
      width="min(420px, 92vw)"
      append-to-body
      destroy-on-close
      :close-on-click-modal="false"
      :close-on-press-escape="!submitting"
      :show-close="!submitting"
      @closed="clearVerification"
    >
      <el-form label-position="top" @submit.prevent="verifyAndReveal">
        <el-form-item label="当前登录密码">
          <el-input
            v-model="verifyPassword"
            type="password"
            autocomplete="current-password"
            placeholder="请输入本人登录密码"
            :disabled="submitting"
            aria-label="当前登录密码"
          />
        </el-form-item>
        <p class="notification-hint">验证后显示 60 秒，隐藏后可重新验证查看。</p>
        <button type="submit" hidden></button>
      </el-form>
      <template #footer>
        <el-button :disabled="submitting" @click="clearVerification">取消</el-button>
        <el-button type="primary" :loading="submitting" :disabled="!verifyPassword" @click="verifyAndReveal">验证并查看</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { usePage } from './SystemNotification.js'

const props = defineProps({
  userId: { type: Number, default: 0 },
  compact: { type: Boolean, default: false }
})

const {
  manager, loading, submitting, error, status, accounts, page, total, form,
  secret, secretAccount, secretVisible, seconds, canManage, stateText, dirty,
  canReveal, verifyVisible, verifyPassword, openVerify, verifyAndReveal, clearVerification,
  loadAccounts, openManage, closeManage, revealAccount, manageVisible,
  refresh, save, rotate, clearSecret, copySecret, beforeLeave
} = usePage(props)

defineExpose({ beforeLeave })
</script>

<style scoped src="./SystemNotification.css"></style>
