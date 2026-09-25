<template>
  <section class="notification-settings" aria-label="系统通知发信">
    <header class="notification-header">
      <div>
        <h2>系统通知发信</h2>
        <p>按账号授权的 SMTP 发信服务，与普通邮箱登录和收发信独立。</p>
      </div>
      <el-button :disabled="submitting || loading" @click="refresh">刷新状态</el-button>
    </header>

    <el-alert
      title="默认关闭，逐账号授权"
      :description="manager ? '只有管理范围内的账号可以授权。管理员修改授权前需要身份确认；关闭时永久撤销旧凭据。' : '普通用户只能查看自己的状态和连接参数。需要开启或重置凭据时，请联系管理员。'"
      type="info"
      show-icon
      :closable="false"
    />

    <div v-if="manager" class="notification-panel">
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
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="primary" :disabled="submitting || loading" @click="selectAccount(row)">管理</el-button>
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
    </div>

    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div v-loading="loading" class="notification-panel" aria-live="polite">
      <template v-if="status">
        <div class="notification-heading">
          <h3>账号：{{ status.account }}</h3>
          <el-tag :type="status.enabled && status.configured ? 'success' : 'info'">{{ stateText }}</el-tag>
        </div>
        <p v-if="!status.account_available" class="notification-warning">账号已禁用或尚未修改临时密码，不能生成或使用通知凭据。</p>

        <el-form v-if="canManage" label-position="top" :disabled="submitting || loading" @submit.prevent="save">
          <el-form-item label="允许该账号通过 SMTP 发送系统通知">
            <el-switch v-model="form.enabled" active-text="开启" inactive-text="关闭" />
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

        <el-descriptions title="使用状态" :column="1" border>
          <el-descriptions-item label="本分钟用量">{{ status.minute_used }} / {{ status.minute_limit }} 个收件人</el-descriptions-item>
          <el-descriptions-item label="今日用量（UTC）">{{ status.day_used }} / {{ status.day_limit }} 个收件人</el-descriptions-item>
          <el-descriptions-item label="单封限制">最多 {{ status.max_recipients }} 个收件人，最大 {{ status.max_message_bytes / 1048576 }} MiB</el-descriptions-item>
        </el-descriptions>
        <p class="notification-hint">配额按提交的收件人数计算，失败和重试也计数；更换凭据或重启不会清空配额。</p>

        <el-descriptions title="应用 SMTP 配置" :column="1" border>
          <el-descriptions-item label="服务器">{{ status.host }}</el-descriptions-item>
          <el-descriptions-item label="隐式 TLS 端口">{{ status.tls_port || '未启用' }}</el-descriptions-item>
          <el-descriptions-item label="STARTTLS 端口">{{ status.starttls_port || '未启用' }}</el-descriptions-item>
          <el-descriptions-item label="SMTP 用户名"><code>{{ status.username }}</code></el-descriptions-item>
          <el-descriptions-item label="SMTP 密码">管理员生成的独立发信凭据，不是网页登录密码或普通客户端授权码</el-descriptions-item>
          <el-descriptions-item label="发件邮箱"><code>{{ status.sender }}</code></el-descriptions-item>
          <el-descriptions-item label="发件名称">{{ status.sender_name }}（服务端固定）</el-descriptions-item>
        </el-descriptions>
        <p class="notification-hint">必须启用加密和证书校验。服务器名需正确解析且由证书覆盖；此处显示配置，不代表公网连通性已验收。</p>
        <el-alert title="仅发信，不提供收件箱" description="通知地址拒收邮件；通知凭据不能用于网页登录、IMAP 或 POP3。普通邮箱及其原有凭据不受影响。" type="warning" :closable="false" show-icon />
      </template>
      <el-empty v-else-if="!loading && !error" description="尚未读取账号状态" />
    </div>

    <el-dialog v-model="secretVisible" title="通知发信凭据（仅显示一次）" width="min(560px, 92vw)" destroy-on-close @closed="clearSecret">
      <p>账号：{{ secretAccount }}。请安全保存并交付给该账号的使用者。</p>
      <el-alert :title="`${seconds} 秒后自动隐藏；关闭后无法再次查看，只能重新生成。`" type="warning" :closable="false" show-icon />
      <el-input :model-value="secret" readonly aria-label="新通知发信凭据" class="notification-secret" />
      <template #footer>
        <el-button @click="copySecret">复制凭据</el-button>
        <el-button type="primary" @click="clearSecret">已保存，关闭</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { usePage } from './SystemNotification.js'

const {
  manager, loading, submitting, error, status, accounts, page, total, form,
  secret, secretAccount, secretVisible, seconds, canManage, stateText, dirty,
  loadAccounts, selectAccount, refresh, save, rotate, clearSecret, copySecret, beforeLeave
} = usePage()

defineExpose({ beforeLeave })
</script>

<style scoped src="./SystemNotification.css"></style>
