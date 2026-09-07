<template>
  <div class="config-page" :data-config-page="selectedSection" v-loading="loading">
    <header class="config-overview" aria-labelledby="config-page-title">
      <div class="overview-heading">
        <div class="overview-copy">
          <div class="overview-title">
            <h3 id="config-page-title">{{ currentSection.label }}</h3>
          </div>
        </div>
        <div class="overview-actions">
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

    <div v-if="availabilityError" class="config-availability" role="status">
      <el-alert :title="availabilityError" type="warning" :closable="false" show-icon />
      <el-button :loading="loading" @click="refreshPage">重新检查</el-button>
    </div>

    <div v-else-if="!loading" class="config-page-body">
        <section class="editor-panel" :aria-busy="sectionLoading">
          <el-alert
            v-if="sectionError"
            :title="sectionError"
            type="error"
            :closable="false"
            show-icon
            class="config-alert"
          />

          <ConfigForm
            v-if="!sectionLoading && !sectionError"
            :section="selectedSection"
            :config="formConfig"
            :errors="formErrors"
            :disabled="!canWrite || !!working || !!pendingCandidate"
            @change="updateField"
          />
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

const {
  availabilityError,
  loading,
  working,
  status,
  selectedSection,
  currentSection,
  sectionLoading,
  formConfig,
  formErrors,
  updateField,
  updateSecret,
  sectionError,
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
