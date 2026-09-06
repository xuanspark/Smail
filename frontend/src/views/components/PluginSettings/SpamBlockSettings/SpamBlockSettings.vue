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

    <el-alert v-if="pendingCandidate" title="存在待应用候选，编辑已锁定。刷新可恢复候选，需要修改时请先取消。" type="warning" :closable="false" show-icon />
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

      <el-checkbox v-if="canWrite()" v-model="confirmed" class="spam-confirmation">
        我已确认这是受控变更，并允许使用最近认证完成高风险配置操作
      </el-checkbox>
      <div class="spam-actions">
        <el-button :loading="working === 'validate'" :disabled="!canWrite() || !!working || !!pendingCandidate || loading" @click="validate">只验证</el-button>
        <el-button type="primary" :loading="working === 'save'" :disabled="!canWrite() || !!working || !!pendingCandidate || loading" @click="save">保存候选配置</el-button>
        <el-button type="success" plain :loading="working === 'apply'" :disabled="!canWrite() || !savedGeneration || !!working || loading" @click="apply">
          应用{{ savedGeneration ? `代次 ${savedGeneration}` : '' }}
        </el-button>
        <el-button v-if="canWrite() && pendingCandidate && !pendingCandidate.sameSession" :disabled="!!working || !confirmed" @click="managePending('resume')">恢复候选到当前会话</el-button>
        <el-button v-if="canWrite() && pendingCandidate" type="warning" plain :disabled="!!working || pendingCandidate.committed" @click="managePending('cancel')">取消候选配置</el-button>
      </div>
      <div v-if="validationResult" class="spam-result" :class="validationResult.valid ? 'is-success' : 'is-error'">
        <strong>{{ validationResult.valid ? '验证通过' : '验证未通过' }}</strong>
        <span v-if="validationResult.generation">候选代次：{{ validationResult.generation }}</span>
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
  confirmed,
  savedGeneration,
  pendingCandidate,
  managePending,
  validationResult,
  settings,
  canWrite,
  load,
  validate,
  save,
  apply
} = useSpamBlockSettings()
</script>

<style scoped src="./SpamBlockSettings.css"></style>
