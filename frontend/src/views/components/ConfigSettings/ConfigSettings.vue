<template>
  <div class="config-settings" v-loading="loading">
    <div class="settings-header">
      <div>
        <h3>配置管理</h3>
        <p class="settings-desc">集中管理服务配置。先验证，再保存与应用，所有变更均可追溯。</p>
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
      <el-button :loading="loading" @click="loadAll">重新检查</el-button>
    </div>

    <div v-else-if="!loading" class="config-workspace">
      <SettingsSubmenu
        title="配置管理"
        caption="配置分区"
        :items="sectionOptions"
        :model-value="selectedSection"
        @select="selectSection"
      />

      <div class="config-main">
        <section class="status-panel">
          <div class="status-heading">
            <div>
              <h4>配置快照</h4>
              <p class="heading-note">当前运行状态与生效版本</p>
            </div>
            <el-button size="small" plain :loading="loading" :disabled="!!working || sectionLoading" @click="loadAll">
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
              <span class="section-eyebrow">分区配置</span>
              <h4>{{ currentSection.label }}</h4>
              <p class="heading-note">{{ currentSection.description }}</p>
            </div>
            <span class="selected-section-label">{{ canWrite ? '候选配置编辑' : '配置查看' }}</span>
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

        <div v-if="(canMigrate && selectedSection === 'base') || (canRestart && status.pendingRestart?.length) || canRollback || canAudit" class="maintenance-heading">
          <h4>维护与记录</h4>
          <p>按需展开，不影响上方配置编辑</p>
        </div>

        <details v-if="canMigrate && selectedSection === 'base'" class="maintenance-panel migration-panel">
          <summary class="maintenance-summary">
            <span class="maintenance-copy">
              <strong>安全迁移数据库</strong>
              <span>复制并校验数据，保留旧数据库</span>
            </span>
            <el-tag type="warning" effect="plain" size="small">高风险操作</el-tag>
            <span class="disclosure-arrow" aria-hidden="true"></span>
          </summary>
          <div class="maintenance-body">
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
              <el-button :loading="working === 'migration-preview'" @click="previewDatabaseMigration">
                生成迁移预览
              </el-button>
              <el-button type="primary" :loading="working === 'migration-execute'" :disabled="!migrationPreview || !migrationToken" @click="executeDatabaseMigration">
                执行迁移
              </el-button>
              <el-button v-if="migrationRecord?.id" plain :loading="working === 'migration-status'" @click="loadMigrationStatus">
                刷新迁移状态
              </el-button>
            </div>
            <div v-if="migrationRecord" class="preview-box migration-result">
              <div class="migration-result-heading">
                <strong>迁移状态</strong>
                <el-tag :type="migrationStatusType(migrationRecord.status)" effect="plain">
                  {{ migrationStatusLabel(migrationRecord.status) }}
                </el-tag>
              </div>
              <span>迁移编号：{{ migrationRecord.id }}</span>
              <span>源数据库：{{ migrationRecord.source?.name || migrationRecord.source?.localRef || '-' }}</span>
              <span>目标数据库：{{ migrationRecord.target?.name || migrationRecord.target?.localRef || '-' }}</span>
              <span v-if="migrationRecord.errorCode" class="migration-error">错误代码：{{ migrationRecord.errorCode }}</span>
            </div>
          </div>
        </details>

        <details v-if="canRestart && status.pendingRestart?.length" class="maintenance-panel restart-panel" open>
          <summary class="maintenance-summary">
            <span class="maintenance-copy">
              <strong>应用待重启服务</strong>
              <span>通过固定的 Smail 服务管理器受控重启</span>
            </span>
            <el-tag type="warning" effect="plain" size="small">待重启</el-tag>
            <span class="disclosure-arrow" aria-hidden="true"></span>
          </summary>
          <div class="maintenance-body">
            <p class="panel-desc">配置变更需要重启时，只能通过固定的 Smail 服务管理器提交请求，不接受外部命令、服务名或参数。</p>
            <div class="tag-list restart-services">
              <el-tag v-for="service in status.pendingRestart" :key="service" type="warning">
                {{ serviceLabel(service) }}
              </el-tag>
            </div>
            <div class="action-row">
              <el-button :loading="working === 'restart-preview'" @click="previewRestart">查看重启预览</el-button>
              <el-button type="danger" :loading="working === 'restart'" :disabled="!restartPreview?.triggerAvailable" @click="requestRestart">
                提交重启请求
              </el-button>
            </div>
            <div v-if="restartPreview" class="preview-box restart-result">
              <strong>重启预览</strong>
              <span>目标：{{ restartPreview.target }} · 操作：{{ restartPreview.action }}</span>
              <span>服务管理器：{{ restartPreview.serviceManager }}</span>
              <span>{{ restartPreview.triggerAvailable ? '重启通道可用' : '重启通道暂不可用，请先检查服务部署' }}</span>
            </div>
          </div>
        </details>

        <details v-if="canRollback" class="maintenance-panel rollback-panel">
          <summary class="maintenance-summary">
            <span class="maintenance-copy">
              <strong>备份与回滚</strong>
              <span>先预览变更，再确认一次性回滚</span>
            </span>
            <el-tag :type="backupsError ? 'warning' : 'info'" effect="plain" size="small">
              {{ backupsError ? '读取失败' : `${backups.length} 份备份` }}
            </el-tag>
            <span class="disclosure-arrow" aria-hidden="true"></span>
          </summary>
          <div class="maintenance-body">
            <div class="panel-heading">
              <p class="heading-note">选择备份并预览回滚范围。</p>
              <el-button size="small" plain :loading="backupsLoading" @click="loadBackups">刷新备份</el-button>
            </div>
            <el-alert v-if="backupsError" :title="backupsError" type="warning" :closable="false" show-icon />
            <el-empty v-else-if="!backups.length && !backupsLoading" description="暂无可用配置备份" :image-size="72" />
            <div v-else class="backup-list">
              <button
                v-for="backup in backups"
                :key="backup.generation"
                type="button"
                class="backup-item"
                :class="{ selected: rollbackGeneration === backup.generation }"
                :aria-pressed="rollbackGeneration === backup.generation"
                @click="rollbackGeneration = backup.generation"
              >
                <span>代次 {{ backup.generation }}</span>
                <small>{{ formatTime(backup.createdAt) }}</small>
              </button>
            </div>
            <el-checkbox v-model="rollbackConfirmed" class="confirmation-check">
              我已阅读回滚预览，并确认执行一次性回滚
            </el-checkbox>
            <div class="action-row">
              <el-button :loading="working === 'preview'" :disabled="!rollbackGeneration" @click="previewRollback">
                预览回滚
              </el-button>
              <el-button type="danger" plain :loading="working === 'rollback'" :disabled="!rollbackPreview || !rollbackConfirmed" @click="executeRollback">
                执行回滚
              </el-button>
            </div>
            <div v-if="rollbackPreview" class="preview-box">
              <strong>回滚预览</strong>
              <span>当前代次 {{ rollbackPreview.currentGeneration }} → 目标代次 {{ rollbackPreview.targetGeneration }}</span>
              <span>候选代次：{{ rollbackPreview.candidateGeneration }}</span>
              <span>变更分区：{{ rollbackPreview.changedSections?.map(sectionLabel).join('、') || '无' }}</span>
            </div>
          </div>
        </details>

        <details v-if="canAudit" class="maintenance-panel audit-panel">
          <summary class="maintenance-summary">
            <span class="maintenance-copy">
              <strong>配置操作审计</strong>
              <span>查询变更记录，按需导出 JSON 或 CSV</span>
            </span>
            <el-tag :type="auditError ? 'warning' : 'info'" effect="plain" size="small">
              {{ auditError ? '读取失败' : `最近 ${auditItems.length} 条` }}
            </el-tag>
            <span class="disclosure-arrow" aria-hidden="true"></span>
          </summary>
          <div class="maintenance-body">
            <div class="panel-heading">
              <p class="heading-note">展示最近 50 条，导出最多 500 条记录。</p>
              <div class="audit-actions">
                <el-button size="small" plain :loading="auditLoading" @click="loadAudit">查询</el-button>
                <el-button v-if="canExportAudit" size="small" type="primary" plain :loading="working === 'export'" @click="exportAudit('json')">
                  导出 JSON
                </el-button>
                <el-button v-if="canExportAudit" size="small" plain :loading="working === 'export-csv'" @click="exportAudit('csv')">
                  导出 CSV
                </el-button>
              </div>
            </div>
            <el-alert v-if="auditError" :title="auditError" type="warning" :closable="false" show-icon />
            <div v-else-if="auditItems.length" class="audit-list">
              <div v-for="item in auditItems" :key="item.id || `${item.createdAt}-${item.operation}`" class="audit-item">
                <div>
                  <strong>{{ operationLabel(item.operation) }}</strong>
                  <span>{{ sectionLabel(item.section) }} · {{ item.actorId || '系统' }}</span>
                </div>
                <time>{{ formatTime(item.createdAt) }}</time>
              </div>
            </div>
            <el-empty v-else-if="!auditLoading" description="暂无配置审计记录" :image-size="72" />
          </div>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup>
import { usePage } from './ConfigSettings.js'
import SettingsSubmenu from '../SettingsSubmenu/SettingsSubmenu.vue'

const {
  availabilityError,
  loading,
  working,
  status,
  stateLabel,
  stateTagType,
  sectionOptions,
  selectedSection,
  currentSection,
  sectionLoading,
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
  backupsError,
  backups,
  rollbackGeneration,
  rollbackConfirmed,
  rollbackPreview,
  auditLoading,
  auditError,
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
</script>

<style scoped src="./ConfigSettings.css"></style>
