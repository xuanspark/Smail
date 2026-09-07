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
              title="上次保存尚未完成，可以继续保存或放弃本次更改。"
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
            <div class="action-row">
              <el-button
                v-if="!pendingCandidate"
                type="primary"
                :loading="working === 'save' || working === 'apply'"
                :disabled="sectionLoading || !!working || !!sectionError || !hasChanges"
                @click="saveAndApply"
              >
                保存配置
              </el-button>
              <el-button
                v-else
                type="primary"
                :loading="working === 'resume' || working === 'apply'"
                :disabled="sectionLoading || !!working || !!sectionError"
                @click="continuePending"
              >
                继续保存
              </el-button>
              <el-button
                v-if="pendingCandidate"
                type="danger"
                plain
                :disabled="!!working || pendingCandidate.committed"
                @click="managePending('cancel')"
              >
                放弃更改
              </el-button>
            </div>
          </div>
          <div v-if="validationResult && !validationResult.valid" class="result-box result-error" role="status">
            <strong>配置检查未通过</strong>
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
  pendingCandidate,
  pendingScopeHint,
  managePending,
  validationResult,
  hasChanges,
  canWrite,
  refreshPage,
  saveAndApply,
  continuePending
} = inject('smail-config-page')
</script>

<style scoped src="../ConfigSettings.css"></style>
<style scoped src="./ConfigEditor.css"></style>
