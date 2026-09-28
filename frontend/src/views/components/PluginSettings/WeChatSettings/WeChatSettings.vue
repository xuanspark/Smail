<template>
  <section v-loading="!!working" class="wechat-settings" aria-label="本人微信通知">
    <header class="wechat-header">
      <div>
        <h4>微信通知</h4>
        <p>绑定本人微信，仅接收当前账号的邮件通知。</p>
      </div>
      <el-tag :type="status.needsRebind ? 'warning' : status.bound ? 'success' : 'info'">
        {{ bindingLabel }}
      </el-tag>
    </header>
    <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
    <el-alert v-if="ready && status.message" :title="status.message" type="warning" :closable="false" show-icon />
    <el-alert
      v-if="status.needsRebind"
      title="系统公众号已变更，请重新扫码绑定。原绑定不会收到新通知。"
      type="warning"
      :closable="false"
      show-icon
    />
    <div class="wechat-identity">
      <span class="wechat-label">绑定微信</span>
      <strong>{{ status.bound ? status.displayName || '已绑定的微信账号' : '尚未绑定' }}</strong>
      <div class="wechat-buttons">
        <el-button :disabled="!ready || !status.available || !!working" @click="beginBinding">
          {{ status.bound ? '重新绑定' : '扫码绑定' }}
        </el-button>
        <el-button v-if="status.bound" type="danger" plain :disabled="!!working" @click="submit('unbind')">
          解除绑定
        </el-button>
      </div>
    </div>
    <el-form label-position="left" label-width="100px" :disabled="!ready || !!working">
      <el-form-item label="邮件通知">
        <el-switch
          v-model="settings.enabled"
          :disabled="!status.bound || (!settings.enabled && (!status.available || status.needsRebind))"
        />
      </el-form-item>
      <el-form-item label="包含正文">
        <el-switch v-model="settings.showContent" :disabled="!status.bound" />
      </el-form-item>
    </el-form>
    <p class="wechat-hint">开启正文后，邮件内容将发送到微信。停用通知不会解除绑定。</p>
    <footer class="wechat-buttons">
      <el-button :disabled="!!working" @click="refresh">重新加载</el-button>
      <el-button
        :disabled="!ready || !!working || !status.available || !status.bound || status.needsRebind || hasChanges || !settings.enabled"
        @click="submit('test')"
      >
        发送测试消息
      </el-button>
      <el-button type="primary" :disabled="!ready || !!working || !hasChanges" @click="submit('save')">
        保存设置
      </el-button>
    </footer>
    <el-dialog
      :model-value="dialogVisible"
      title="绑定本人微信"
      width="420px"
      append-to-body
      :close-on-click-modal="false"
      :close-on-press-escape="!working"
      :show-close="!working"
      :before-close="closeBinding"
    >
      <div class="wechat-binding" aria-live="polite">
        <el-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon />
        <template v-if="binding.phase === 'claimed'">
          <h4>请核对微信身份</h4>
          <strong>{{ binding.displayName || '已授权的微信账号' }}</strong>
          <p>确认是本人后完成绑定；不是本人请取消。</p>
        </template>
        <template v-else-if="binding.phase !== 'expired'">
          <img v-if="binding.image" :src="binding.image" width="240" height="240" alt="本人微信绑定二维码" />
          <p>{{ binding.phase === 'processing' ? '正在读取微信授权结果…' : '请用本人微信扫码授权，再回到此处确认。' }}</p>
          <span class="wechat-hint">二维码有效期为 5 分钟，请勿转发。</span>
        </template>
      </div>
      <template #footer>
        <div class="wechat-buttons wechat-dialog-actions">
          <el-button :disabled="!!working" @click="closeBinding">取消</el-button>
          <el-button
            v-if="bindingError && binding.phase !== 'expired'"
            :disabled="!!working || polling"
            @click="poll"
          >
            重试状态查询
          </el-button>
          <el-button
            v-if="binding.phase === 'claimed'"
            type="primary"
            :disabled="!!working"
            @click="confirmBinding"
          >
            确认绑定
          </el-button>
        </div>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { useWeChatSettings } from './WeChatSettings.js'

const {
  settings, status, binding, ready, working, errorMessage, bindingError,
  dialogVisible, polling, hasChanges, bindingLabel, refresh, submit,
  beginBinding, confirmBinding, closeBinding, poll, beforeLeave
} = useWeChatSettings()

defineExpose({ beforeLeave })
</script>

<style scoped src="./WeChatSettings.css"></style>
