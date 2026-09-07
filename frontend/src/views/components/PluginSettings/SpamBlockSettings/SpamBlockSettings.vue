<template>
  <section class="spam-block-settings" v-loading="loading">
    <div class="plugin-heading">
      <div>
        <span class="plugin-eyebrow">安全插件</span>
        <h4>垃圾邮件拦截</h4>
        <p>配置模型接口和识别阈值，配置会通过统一配置管理器校验后再应用。</p>
      </div>
      <el-button size="small" plain :loading="loading" :disabled="!!working" @click="load">刷新</el-button>
    </div>

    <el-alert
      v-if="!canWrite()"
      title="当前账号仅能查看配置，保存和应用操作已禁用。"
      type="info"
      :closable="false"
      show-icon
    />
    <el-alert
      v-if="errorMessage"
      class="spam-alert"
      :title="errorMessage"
      type="error"
      :closable="false"
      show-icon
    />

    <el-alert v-if="pendingCandidate" title="上次保存尚未完成，可以继续保存或放弃本次更改。" type="warning" :closable="false" show-icon />
    <el-form label-position="top" class="spam-form">
      <el-form-item label="模型 API 接口地址">
        <el-input
          v-model="settings.url"
          :disabled="!canWrite() || !!working || !!pendingCandidate || loading"
          placeholder="例如：https://model.example.com/v1/predict"
          clearable
        />
      </el-form-item>
      <div class="spam-form-grid">
        <el-form-item label="请求超时时间（毫秒）">
          <el-input-number
            v-model="settings.timeout"
            :disabled="!canWrite() || !!working || !!pendingCandidate || loading"
            :min="100"
            :max="120000"
            :step="100"
            controls-position="right"
          />
        </el-form-item>
        <el-form-item label="识别阈值">
          <el-input-number
            v-model="settings.threshold"
            :disabled="!canWrite() || !!working || !!pendingCandidate || loading"
            :min="0"
            :max="100"
            :step="0.01"
            :precision="2"
            controls-position="right"
          />
        </el-form-item>
      </div>
      <p class="spam-help">阈值越低，过滤越严格，也更容易产生误判；请根据模型返回结果逐步调整。</p>

      <div class="spam-actions">
        <el-button
          v-if="!pendingCandidate"
          type="primary"
          :loading="working === 'save' || working === 'apply'"
          :disabled="!canWrite() || !!working || loading || !hasChanges"
          @click="saveAndApply"
        >
          保存配置
        </el-button>
        <el-button
          v-else
          type="primary"
          :loading="working === 'resume' || working === 'apply'"
          :disabled="!canWrite() || !!working || loading"
          @click="continuePending"
        >
          继续保存
        </el-button>
        <el-button v-if="canWrite() && pendingCandidate" type="danger" plain :disabled="!!working || pendingCandidate.committed" @click="managePending('cancel')">放弃更改</el-button>
      </div>
      <div v-if="validationResult && !validationResult.valid" class="spam-result is-error">
        <strong>配置检查未通过</strong>
        <ul v-if="validationResult.issues?.length">
          <li v-for="issue in validationResult.issues" :key="`${issue.code}-${issue.message}`">{{ issue.code }}：{{ issue.message }}</li>
        </ul>
      </div>
    </el-form>
  </section>
</template>

<script setup>
import { useSpamBlockSettings } from './SpamBlockSettings.js'

const {
  loading,
  working,
  errorMessage,
  pendingCandidate,
  managePending,
  validationResult,
  settings,
  hasChanges,
  canWrite,
  load,
  saveAndApply,
  continuePending
} = useSpamBlockSettings()
</script>

<style scoped src="./SpamBlockSettings.css"></style>
