<template>
  <section v-loading="!!working" class="telegram-settings" aria-label="本人 Telegram 通知">
    <header class="telegram-header">
      <div>
        <h4>Telegram 通知</h4>
        <p>绑定本人 Telegram，仅接收当前账号的邮件通知。</p>
      </div>
      <el-tag :type="status.needsRebind ? 'warning' : status.bound ? 'success' : 'info'">
        {{ bindingLabel }}
      </el-tag>
    </header>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-alert v-if="ready && status.message" :title="status.message" type="warning" :closable="false" show-icon />
    <el-alert
      v-if="status.needsRebind"
      title="机器人已变更或原绑定未经授权，请重新绑定后再接收通知。"
      type="warning"
      :closable="false"
      show-icon
    />
    <div class="telegram-identity">
      <span class="telegram-hint">绑定账号</span>
      <strong>{{ status.bound ? status.displayName || '待重新授权的账号' : '尚未绑定' }}</strong>
      <div class="telegram-buttons">
        <el-button :disabled="!ready || !status.available || !!working" @click="beginBinding">
          {{ status.bound ? '重新绑定' : '授权绑定' }}
        </el-button>
        <el-button v-if="status.bound" type="danger" plain :disabled="!!working" @click="submit('unbind')">
          解除绑定
        </el-button>
      </div>
    </div>
    <el-form label-position="left" label-width="120px" :disabled="!ready || !!working">
      <el-form-item label="邮件通知">
        <el-switch
          v-model="settings.enabled"
          :disabled="!status.bound || (!settings.enabled && (!status.available || status.needsRebind))"
        />
      </el-form-item>
      <el-form-item label="显示邮件内容">
        <el-switch v-model="settings.showContent" :disabled="!status.bound" />
      </el-form-item>
      <el-form-item label="点击显示正文">
        <el-switch v-model="settings.spoilerContent" :disabled="!status.bound || !settings.showContent" />
      </el-form-item>
      <el-form-item label="发送附件">
        <el-switch v-model="settings.sendAttachments" :disabled="!status.bound" />
      </el-form-item>
      <el-form-item label="禁用链接预览">
        <el-switch v-model="settings.disableLinkPreview" :disabled="!status.bound" />
      </el-form-item>
    </el-form>
    <p class="telegram-hint">开启正文或附件后，相应内容将发送到 Telegram。停用通知不会解除绑定。</p>
    <footer class="telegram-actions">
      <el-button :disabled="!!working" @click="refresh">重新加载</el-button>
      <el-button
        :disabled="!ready || !!working || !status.available || !status.bound || status.needsRebind || hasChanges || !settings.enabled"
        @click="submit('test')"
      >
        发送测试消息
      </el-button>
      <el-button type="primary" :disabled="!ready || !!working || !hasChanges" @click="submit('save')">保存设置</el-button>
    </footer>
    <el-dialog
      :model-value="dialogVisible"
      title="绑定本人 Telegram"
      width="420px"
      append-to-body
      :close-on-click-modal="false"
      :close-on-press-escape="!working"
      :show-close="!working"
      :before-close="closeBinding"
    >
      <div class="telegram-binding" aria-live="polite">
        <el-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon />
        <template v-if="binding.phase === 'claimed'">
          <h4>请核对 Telegram 身份</h4>
          <strong>{{ binding.displayName || '已授权的 Telegram 账号' }}</strong>
          <p>确认是本人后完成绑定；不是本人请取消。</p>
        </template>
        <template v-else-if="binding.phase !== 'expired'">
          <img v-if="binding.image" :src="binding.image" width="240" height="240" alt="本人 Telegram 授权二维码" />
          <a v-if="binding.url" :href="binding.url" target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer">
            打开 Telegram 授权
          </a>
          <p>点击链接或扫码，在机器人私聊中点击“开始”，再回到此处确认。</p>
          <span class="telegram-hint">链接有效期为 5 分钟，请勿转发。</span>
        </template>
      </div>
      <template #footer>
        <div class="telegram-buttons telegram-dialog-actions">
          <el-button :disabled="!!working" @click="closeBinding">取消</el-button>
          <el-button v-if="bindingError && binding.phase !== 'expired'" :disabled="!!working || polling" @click="poll">
            重试状态查询
          </el-button>
          <el-button v-if="binding.phase === 'claimed'" type="primary" :disabled="!!working" @click="confirmBinding">
            确认绑定
          </el-button>
        </div>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { useTelegramSettings } from './TelegramSettings.js'

const {
  settings, status, binding, ready, working, errorMessage, bindingError,
  dialogVisible, polling, hasChanges, bindingLabel, refresh, submit,
  beginBinding, confirmBinding, closeBinding, poll, beforeLeave
} = useTelegramSettings()

defineExpose({ beforeLeave })
</script>

<style scoped src="./TelegramSettings.css"></style>
