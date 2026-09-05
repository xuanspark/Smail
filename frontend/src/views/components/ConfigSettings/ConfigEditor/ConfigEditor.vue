<template>
  <div class="config-page" :data-config-page="selectedSection" v-loading="loading">
    <div class="settings-header">
      <div>
        <h3>{{ currentSection.label }}</h3>
        <p class="settings-desc">{{ currentSection.description }}</p>
      </div>
      <el-tag class="access-tag" :type="canWrite ? 'info' : 'warning'" effect="plain">
        {{ canWrite ? '敏感凭据已脱敏' : '只读模式' }}
      </el-tag>
    </div>

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
        <section class="status-panel">
          <div class="status-heading">
            <div>
              <h4>配置快照</h4>
              <p class="heading-note">当前运行状态与生效版本</p>
            </div>
            <el-button size="small" plain :loading="loading" :disabled="!!working || sectionLoading" @click="refreshPage">
              刷新状态
            </el-button>
          </div>
          <div class="status-grid">
            <div class="status-item">
              <span class="status-label">运行状态</span>
              <el-tag :type="stateTagType" effect="light">{{ stateLabel }}</el-tag>
            </div>
            <div class="status-item">
              <span class="status-label">当前生效代次</span>
              <strong class="generation-value">{{ status.activeGeneration || '-' }}</strong>
            </div>
            <div class="status-item">
              <span class="status-label">最近校验时间</span>
              <strong>{{ formatTime(status.lastValidAt) }}</strong>
            </div>
            <div class="status-item" :class="{ 'status-warning': status.pendingRestart?.length }">
              <span class="status-label">待重启服务</span>
              <div v-if="status.pendingRestart?.length" class="tag-list">
                <el-tag v-for="service in status.pendingRestart" :key="service" type="warning" effect="plain">
                  {{ serviceLabel(service) }}
                </el-tag>
              </div>
              <strong v-else>无需重启</strong>
            </div>
          </div>
          <el-alert
            v-for="issue in status.degradedReasons || []"
            :key="`${issue.code}-${issue.message}`"
            :title="`${issue.code}：${issue.message}`"
            type="warning"
            :closable="false"
            show-icon
            class="degraded-alert"
          />
        </section>

        <section class="editor-panel" :aria-busy="sectionLoading">
          <div class="panel-heading">
            <div>
              <h4>{{ canWrite ? '编辑配置' : '查看配置' }}</h4>
              <p class="heading-note">{{ canWrite ? '仅处理当前页面的配置，保存后再应用生效。' : '当前页面的脱敏配置内容。' }}</p>
            </div>
            <span class="selected-section-label">{{ currentSection.label }}</span>
          </div>

          <ol v-if="canWrite" class="change-workflow" aria-label="配置变更流程">
            <li><span class="step-number">1</span><span>验证配置</span></li>
            <li><span class="step-number">2</span><span>保存候选</span></li>
            <li><span class="step-number">3</span><span>应用生效</span></li>
          </ol>

          <el-alert
            v-if="sectionError"
            :title="sectionError"
            type="error"
            :closable="false"
            show-icon
            class="config-alert"
          />

          <div class="editor-shell" v-loading="sectionLoading">
            <div class="editor-toolbar">
              <strong>配置内容 <span class="format-label">JSON</span></strong>
              <span>编辑代次 {{ status.activeGeneration || '-' }}</span>
            </div>
            <el-input
              v-model="editorText"
              type="textarea"
              :rows="12"
              resize="vertical"
              spellcheck="false"
              wrap="off"
              class="config-editor"
              :readonly="!canWrite || sectionLoading || !!working || !!sectionError"
              aria-label="配置 JSON 编辑器"
              aria-describedby="config-sensitive-hint"
            />
          </div>
          <p id="config-sensitive-hint" class="editor-hint">{{ sensitiveHint }}。</p>

          <div v-if="secretField" class="secret-editor">
            <div>
              <label for="config-secret-input">{{ secretField.label }}</label>
              <p>{{ secretField.description }}</p>
            </div>
            <el-input
              id="config-secret-input"
              v-model="secretValue"
              type="password"
              show-password
              clearable
              :disabled="!canWrite || sectionLoading || !!working || !!sectionError"
              :placeholder="secretField.placeholder"
              autocomplete="new-password"
            />
          </div>

          <div v-if="canWrite" class="editor-footer">
            <el-checkbox v-model="confirmed" class="confirmation-check" :disabled="sectionLoading || !!working || !!sectionError">
              我已确认这是受控变更，并允许使用最近认证完成高风险配置操作
            </el-checkbox>
            <div class="action-row">
              <el-button :loading="working === 'validate'" :disabled="sectionLoading || !!working || !!sectionError" @click="validateConfig">
                只验证
              </el-button>
              <el-button type="primary" :loading="working === 'save'" :disabled="sectionLoading || !!working || !!sectionError" @click="saveConfig">
                保存候选配置
              </el-button>
              <el-button type="success" plain :loading="working === 'apply'" :disabled="sectionLoading || !!working || !savedGeneration" @click="applyConfig">
                {{ savedGeneration ? `应用代次 ${savedGeneration}` : '应用配置' }}
              </el-button>
            </div>
            <p class="action-hint">保存不会立即生效，确认候选配置后再应用。</p>
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

const {
  availabilityError,
  loading,
  working,
  status,
  stateLabel,
  stateTagType,
  selectedSection,
  currentSection,
  sectionLoading,
  editorText,
  sectionError,
  sensitiveHint,
  secretField,
  secretValue,
  confirmed,
  savedGeneration,
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
