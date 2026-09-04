<template>
  <div class="config-settings" v-loading="loading">
    <div class="settings-header">
      <h3>配置管理</h3>
      <p class="settings-desc">按配置分区安全编辑、验证、应用和回滚，敏感凭据只显示状态，不会回显明文。</p>
    </div>

    <el-alert
      v-if="!canWrite"
      title="当前账号仅能查看配置，保存、应用和回滚操作已隐藏。"
      type="info"
      :closable="false"
      show-icon
      class="config-alert"
    />

    <div class="config-workspace">
      <nav class="config-submenu" aria-label="配置分区">
        <button
          v-for="item in sectionOptions"
          :key="item.value"
          type="button"
          class="config-submenu-item"
          :class="{ active: selectedSection === item.value }"
          :aria-current="selectedSection === item.value ? 'page' : undefined"
          @click="selectSection(item.value)"
        >
          {{ item.label }}
        </button>
      </nav>

      <div class="config-main">
        <section class="status-panel">
          <div class="status-heading">
            <div>
              <span class="section-eyebrow">运行状态</span>
              <h4>配置快照</h4>
            </div>
            <el-button size="small" plain :loading="loading" @click="loadAll">刷新</el-button>
          </div>
          <div class="status-grid">
            <div class="status-item">
              <span>状态</span>
              <el-tag :type="stateTagType">{{ stateLabel }}</el-tag>
            </div>
            <div class="status-item">
              <span>当前生效代次</span>
              <strong>{{ status.activeGeneration || '-' }}</strong>
            </div>
            <div class="status-item">
              <span>最近校验时间</span>
              <strong>{{ formatTime(status.lastValidAt) }}</strong>
            </div>
            <div class="status-item status-wide">
              <span>待重启服务</span>
              <div v-if="status.pendingRestart?.length" class="tag-list">
                <el-tag v-for="service in status.pendingRestart" :key="service" type="warning" effect="plain">
                  {{ serviceLabel(service) }}
                </el-tag>
              </div>
              <strong v-else>无</strong>
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

        <section class="editor-panel">
          <div class="panel-heading">
            <div>
              <span class="section-eyebrow">分区配置</span>
              <h4>编辑候选配置</h4>
            </div>
            <strong class="selected-section-label">{{ sectionOptions.find(item => item.value === selectedSection)?.label }}</strong>
          </div>

      <el-alert
        v-if="sectionError"
        :title="sectionError"
        type="error"
        :closable="false"
        show-icon
        class="config-alert"
      />

      <div class="editor-meta">
        <span>编辑代次：{{ status.activeGeneration || '-' }}</span>
        <span>敏感字段：{{ sensitiveHint }}</span>
      </div>
      <el-input
        v-model="editorText"
        type="textarea"
        :rows="16"
        resize="vertical"
        spellcheck="false"
        class="config-editor"
        :readonly="!canWrite"
        aria-label="配置 JSON 编辑器"
      />

      <div v-if="secretField" class="secret-editor">
        <div>
          <strong>{{ secretField.label }}</strong>
          <p>{{ secretField.description }}</p>
        </div>
        <el-input
          v-model="secretValue"
          type="password"
          show-password
          clearable
          :disabled="!canWrite"
          :placeholder="secretField.placeholder"
          autocomplete="new-password"
        />
      </div>

      <el-checkbox v-if="canWrite" v-model="confirmed" class="confirmation-check">
        我已确认这是受控变更，并允许使用最近认证完成高风险配置操作
      </el-checkbox>
      <div class="action-row">
        <el-button :loading="working === 'validate'" :disabled="!canWrite" @click="validateConfig">只验证</el-button>
        <el-button type="primary" :loading="working === 'save'" :disabled="!canWrite" @click="saveConfig">保存候选配置</el-button>
        <el-button type="success" plain :loading="working === 'apply'" :disabled="!canWrite || !savedGeneration" @click="applyConfig">
          应用 {{ savedGeneration ? `代次 ${savedGeneration}` : '' }}
        </el-button>
      </div>
      <div v-if="validationResult" class="result-box" :class="validationResult.valid ? 'result-success' : 'result-error'">
        <strong>{{ validationResult.valid ? '验证通过' : '验证未通过' }}</strong>
        <span v-if="validationResult.generation">候选代次：{{ validationResult.generation }}</span>
        <ul v-if="validationResult.issues?.length">
          <li v-for="issue in validationResult.issues" :key="`${issue.code}-${issue.message}`">{{ issue.code }}：{{ issue.message }}</li>
        </ul>
      </div>
        </section>

        <section v-if="canMigrate" class="migration-panel">
      <div class="panel-heading">
        <div>
          <span class="section-eyebrow">数据库迁移</span>
          <h4>安全迁移数据库</h4>
        </div>
        <el-tag type="warning" effect="plain">仅允许修改数据库名称</el-tag>
      </div>
      <p class="panel-desc">系统会先生成一次性预览票据，再复制并校验数据。主机、端口、用户名和密码保持不变，旧数据库不会自动删除。</p>
      <div class="migration-source">
        <div>
          <span>当前数据库类型</span>
          <strong>{{ databaseTypeLabel(databaseConfig.type) }}</strong>
        </div>
        <div>
          <span>当前数据库</span>
          <strong>{{ databaseConfig.type === 'sqlite' ? databaseConfig.localRef || '-' : databaseConfig.name || '-' }}</strong>
        </div>
      </div>
      <el-form label-position="top" class="migration-form">
        <el-form-item :label="databaseConfig.type === 'sqlite' ? '目标数据库文件引用' : '目标数据库名称'">
          <el-input
            v-model="migrationTargetName"
            :placeholder="databaseConfig.type === 'sqlite' ? '例如 smail-next.db' : '例如 smail_next'"
            maxlength="128"
            clearable
          />
        </el-form-item>
      </el-form>
      <div class="action-row">
        <el-button :loading="working === 'migration-preview'" @click="previewDatabaseMigration">生成迁移预览</el-button>
        <el-button type="primary" :loading="working === 'migration-execute'" :disabled="!migrationPreview || !migrationToken" @click="executeDatabaseMigration">
          执行迁移
        </el-button>
        <el-button v-if="migrationRecord?.id" plain :loading="working === 'migration-status'" @click="loadMigrationStatus">刷新迁移状态</el-button>
      </div>
      <div v-if="migrationRecord" class="preview-box migration-result">
        <div class="migration-result-heading">
          <strong>迁移状态</strong>
          <el-tag :type="migrationStatusType(migrationRecord.status)" effect="plain">{{ migrationStatusLabel(migrationRecord.status) }}</el-tag>
        </div>
        <span>迁移编号：{{ migrationRecord.id }}</span>
        <span>源数据库：{{ migrationRecord.source?.name || migrationRecord.source?.localRef || '-' }}</span>
        <span>目标数据库：{{ migrationRecord.target?.name || migrationRecord.target?.localRef || '-' }}</span>
        <span v-if="migrationRecord.errorCode" class="migration-error">错误代码：{{ migrationRecord.errorCode }}</span>
      </div>
        </section>

        <section v-if="canRestart && status.pendingRestart?.length" class="restart-panel">
      <div class="panel-heading">
        <div>
          <span class="section-eyebrow">受控重启</span>
          <h4>应用待重启服务</h4>
        </div>
        <el-tag type="warning" effect="plain">固定目标：Smail</el-tag>
      </div>
      <p class="panel-desc">配置变更需要重启时，只能通过固定的 Smail 服务管理器提交请求，不接受外部命令、服务名或参数。</p>
      <div class="tag-list restart-services">
        <el-tag v-for="service in status.pendingRestart" :key="service" type="warning">{{ serviceLabel(service) }}</el-tag>
      </div>
      <div class="action-row">
        <el-button :loading="working === 'restart-preview'" @click="previewRestart">查看重启预览</el-button>
        <el-button type="danger" :loading="working === 'restart'" :disabled="!restartPreview?.triggerAvailable" @click="requestRestart">提交重启请求</el-button>
      </div>
      <div v-if="restartPreview" class="preview-box restart-result">
        <strong>重启预览</strong>
        <span>目标：{{ restartPreview.target }} · 操作：{{ restartPreview.action }}</span>
        <span>服务管理器：{{ restartPreview.serviceManager }}</span>
        <span>{{ restartPreview.triggerAvailable ? '重启通道可用' : '重启通道暂不可用，请先检查服务部署' }}</span>
      </div>
        </section>

        <section v-if="canRollback" class="rollback-panel">
      <div class="panel-heading">
        <div>
          <span class="section-eyebrow">受控回滚</span>
          <h4>备份与回滚</h4>
        </div>
        <el-button size="small" plain :loading="backupsLoading" @click="loadBackups">刷新备份</el-button>
      </div>
      <el-empty v-if="!backups.length && !backupsLoading" description="暂无可用配置备份" :image-size="72" />
      <div v-else class="backup-list">
        <button
          v-for="backup in backups"
          :key="backup.generation"
          type="button"
          class="backup-item"
          :class="{ selected: rollbackGeneration === backup.generation }"
          @click="rollbackGeneration = backup.generation"
        >
          <span>代次 {{ backup.generation }}</span>
          <small>{{ formatTime(backup.createdAt) }}</small>
        </button>
      </div>
      <el-checkbox v-model="rollbackConfirmed" class="confirmation-check">我已阅读回滚预览，并确认执行一次性回滚</el-checkbox>
      <div class="action-row">
        <el-button :loading="working === 'preview'" :disabled="!rollbackGeneration" @click="previewRollback">预览回滚</el-button>
        <el-button type="danger" plain :loading="working === 'rollback'" :disabled="!rollbackPreview || !rollbackConfirmed" @click="executeRollback">执行回滚</el-button>
      </div>
      <div v-if="rollbackPreview" class="preview-box">
        <strong>回滚预览</strong>
        <span>当前代次 {{ rollbackPreview.currentGeneration }} → 目标代次 {{ rollbackPreview.targetGeneration }}</span>
        <span>候选代次：{{ rollbackPreview.candidateGeneration }}</span>
        <span>变更分区：{{ rollbackPreview.changedSections?.map(sectionLabel).join('、') || '无' }}</span>
      </div>
        </section>

        <section v-if="canAudit" class="audit-panel">
      <div class="panel-heading">
        <div>
          <span class="section-eyebrow">审计记录</span>
          <h4>配置操作审计</h4>
        </div>
        <div class="audit-actions">
          <el-button size="small" plain :loading="auditLoading" @click="loadAudit">查询</el-button>
          <el-button v-if="canExportAudit" size="small" type="primary" plain :loading="working === 'export'" @click="exportAudit('json')">导出 JSON</el-button>
          <el-button v-if="canExportAudit" size="small" plain :loading="working === 'export-csv'" @click="exportAudit('csv')">导出 CSV</el-button>
        </div>
      </div>
      <div v-if="auditItems.length" class="audit-list">
        <div v-for="item in auditItems" :key="item.id || `${item.createdAt}-${item.operation}`" class="audit-item">
          <div>
            <strong>{{ operationLabel(item.operation) }}</strong>
            <span>{{ sectionLabel(item.section) }} · {{ item.actorId || '系统' }}</span>
          </div>
          <time>{{ formatTime(item.createdAt) }}</time>
        </div>
      </div>
      <el-empty v-else description="暂无配置审计记录" :image-size="72" />
        </section>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { usePage } from './ConfigSettings.js'

const {
  loading,
  working,
  status,
  stateLabel,
  stateTagType,
  sectionOptions,
  selectedSection,
  selectSection,
  editorText,
  sectionError,
  sensitiveHint,
  secretField,
  secretValue,
  confirmed,
  savedGeneration,
  validationResult,
  canWrite,
  canRollback,
  canAudit,
  canExportAudit,
  backupsLoading,
  backups,
  rollbackGeneration,
  rollbackConfirmed,
  rollbackPreview,
  auditLoading,
  auditItems,
  databaseConfig,
  migrationTargetName,
  migrationPreview,
  migrationToken,
  migrationRecord,
  migrationStatusLabel,
  migrationStatusType,
  databaseTypeLabel,
  canMigrate,
  canRestart,
  restartPreview,
  loadAll,
  validateConfig,
  saveConfig,
  applyConfig,
  loadBackups,
  previewDatabaseMigration,
  loadMigrationStatus,
  executeDatabaseMigration,
  previewRestart,
  requestRestart,
  previewRollback,
  executeRollback,
  loadAudit,
  exportAudit,
  formatTime,
  serviceLabel,
  sectionLabel,
  operationLabel
} = usePage()

void computed
</script>

<style scoped src="./ConfigSettings.css"></style>
