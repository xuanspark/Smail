<template>
  <div class="config-page" :data-config-page="selectedSection" v-loading="loading">
    <header class="config-overview" aria-labelledby="config-page-title">
      <div class="overview-heading">
        <div class="overview-copy">
          <div class="overview-title">
            <h3 id="config-page-title">{{ currentSection.label }}</h3>
            <el-tag v-if="!loading && !availabilityError" :type="stateTagType" effect="light" size="small">
              <span class="sr-only">运行状态：</span>{{ stateLabel }}
            </el-tag>
          </div>
          <p class="settings-desc">{{ currentSection.description }}</p>
        </div>
        <div class="overview-actions">
          <el-tag class="access-tag" :type="canWrite ? 'info' : 'warning'" effect="plain" size="small">
            {{ canWrite ? '敏感凭据已脱敏' : '只读模式' }}
          </el-tag>
          <el-button
            v-if="!availabilityError"
            size="small"
            plain
            :loading="loading"
            :disabled="!!working || sectionLoading || loading"
            @click="refreshPage"
          >
            刷新状态
          </el-button>
        </div>
      </div>
      <template v-if="!loading && !availabilityError">
        <dl class="overview-meta" aria-label="当前配置状态">
          <div class="overview-meta-item">
            <dt>生效代次</dt>
            <dd>{{ status.activeGeneration || '-' }}</dd>
          </div>
          <div class="overview-meta-item">
            <dt>最近校验</dt>
            <dd>{{ formatTime(status.lastValidAt) }}</dd>
          </div>
          <div class="overview-meta-item" :class="{ 'overview-meta-warning': status.pendingRestart?.length }">
            <dt>待重启服务</dt>
            <dd v-if="status.pendingRestart?.length" class="tag-list">
              <el-tag v-for="service in status.pendingRestart" :key="service" type="warning" effect="plain" size="small">
                {{ serviceLabel(service) }}
              </el-tag>
            </dd>
            <dd v-else>无需重启</dd>
          </div>
        </dl>
        <el-alert
          v-for="issue in status.degradedReasons || []"
          :key="`${issue.code}-${issue.message}`"
          :title="`${issue.code}：${issue.message}`"
          type="warning"
          :closable="false"
          show-icon
          class="degraded-alert"
        />
      </template>
    </header>

    <el-alert
      v-if="!canWrite"
      title="当前账号仅能查看配置，保存、应用和回滚操作已隐藏。"
      type="info"
      :closable="false"
      show-icon
      class="config-alert"
    />

    <div v-if="availabilityError" class="config-availability" role="status">
      <el-alert :title="availabilityError" type="warning" :closable="false" show-icon />
      <el-button :loading="loading" @click="refreshPage">重新检查</el-button>
    </div>

    <div v-else-if="!loading" class="config-page-body">
        <section class="editor-panel" :aria-busy="sectionLoading">
          <p v-if="selectedSection === 'ssl' && status.tlsMode === 'proxy'" class="heading-note" role="note">
            当前由反向代理提供 HTTPS，Smail 不自动申请或续期 SSL 证书。下方配置仅用于本地邮件服务证书；外部续期后请重新导入并应用，不能仅更新反代服务器上的证书。
          </p>

          <el-alert
            v-if="sectionError"
            :title="sectionError"
            type="error"
            :closable="false"
            show-icon
            class="config-alert"
          />

          <MailPortSummary v-if="mailPortSummary && !sectionLoading && !sectionError" :services="mailPortSummary" />
          <ConfigForm
            v-if="!sectionLoading && !sectionError"
            :section="selectedSection"
            :config="formConfig"
            :errors="formErrors"
            :disabled="!canWrite || !!working || !!pendingCandidate"
            @change="updateField"
          />
          <p id="config-sensitive-hint" class="editor-hint">{{ sensitiveHint }}。</p>

          <div v-if="secretField" class="secret-editor">
            <div>
              <label for="config-secret-input">{{ secretField.label }}</label>
              <p>{{ secretField.description }}</p>
            </div>
            <el-input
              id="config-secret-input"
              :model-value="secretValue"
              type="password"
              show-password
              clearable
              :disabled="!canWrite || sectionLoading || !!working || !!sectionError || !!pendingCandidate"
              :placeholder="secretField.placeholder"
              autocomplete="new-password"
              @update:model-value="updateSecret"
            />
          </div>

          <div v-if="canWrite" class="editor-footer">
            <el-alert
              v-if="pendingCandidate"
              :title="`服务器存在待应用候选，代次 ${pendingCandidate.generation}。已锁定编辑；如需修改，请先取消候选。刷新页面不会丢失候选。`"
              type="warning"
              :closable="false"
              show-icon
            />
            <el-alert
              v-if="pendingScopeHint"
              :title="pendingScopeHint"
              type="warning"
              :closable="false"
              show-icon
            />
            <el-checkbox v-model="confirmed" class="confirmation-check" :disabled="sectionLoading || !!working || !!sectionError">
              我已确认这是受控变更，并允许使用最近认证完成高风险配置操作
            </el-checkbox>
            <div class="action-row">
              <el-button :loading="working === 'validate'" :disabled="sectionLoading || !!working || !!sectionError || !!pendingCandidate" @click="validateConfig">
                只验证
              </el-button>
              <el-button type="primary" :loading="working === 'save'" :disabled="sectionLoading || !!working || !!sectionError || !!pendingCandidate" @click="saveConfig">
                保存候选配置
              </el-button>
              <el-button v-if="pendingCandidate && !pendingCandidate.sameSession" :disabled="!!working || !confirmed" @click="managePending('resume')">
                恢复候选到当前会话
              </el-button>
              <el-button v-if="pendingCandidate" type="warning" plain :disabled="!!working || pendingCandidate.committed" @click="managePending('cancel')">
                取消候选配置
              </el-button>
              <el-button type="success" plain :loading="working === 'apply'" :disabled="sectionLoading || !!working || !savedGeneration" @click="applyConfig">
                {{ savedGeneration ? `应用代次 ${savedGeneration}` : '应用配置' }}
              </el-button>
            </div>
            <p class="action-hint">保存不会立即生效；需要继续修改时先取消候选。跨会话恢复或取消需要近期认证并勾选确认。</p>
          </div>
          <div v-if="validationResult" class="result-box" :class="validationResult.valid ? 'result-success' : 'result-error'" role="status">
            <strong>{{ validationResult.valid ? '验证通过' : '验证未通过' }}</strong>
            <span v-if="validationResult.generation">候选代次：{{ validationResult.generation }}</span>
            <ul v-if="validationResult.issues?.length">
              <li v-for="issue in validationResult.issues" :key="`${issue.code}-${issue.message}`">
                {{ issue.code }}：{{ issue.message }}
              </li>
            </ul>
          </div>
        </section>

    </div>
  </div>
</template>

<script setup>
import { inject } from 'vue'
import ConfigForm from '../ConfigForm/ConfigForm.vue'
import MailPortSummary from '../MailPortSummary/MailPortSummary.vue'

const {
  mailPortSummary,
  availabilityError,
  loading,
  working,
  status,
  stateLabel,
  stateTagType,
  selectedSection,
  currentSection,
  sectionLoading,
  formConfig,
  formErrors,
  updateField,
  updateSecret,
  sectionError,
  sensitiveHint,
  secretField,
  secretValue,
  confirmed,
  savedGeneration,
  pendingCandidate,
  pendingScopeHint,
  managePending,
  validationResult,
  canWrite,
  refreshPage,
  validateConfig,
  saveConfig,
  applyConfig,
  formatTime,
  serviceLabel
} = inject('smail-config-page')
</script>

<style scoped src="../ConfigSettings.css"></style>
<style scoped src="./ConfigEditor.css"></style>
