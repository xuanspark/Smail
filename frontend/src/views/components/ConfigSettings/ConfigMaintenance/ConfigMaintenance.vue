<template>
  <div class="config-page" :data-config-page="pageKey" v-loading="loading">
    <div class="settings-header">
      <div>
        <h3>{{ pageTitle }}</h3>
        <p class="settings-desc">维护工具独立运行，不会修改其他页面的编辑状态。</p>
      </div>
      <el-button plain :disabled="!!working" @click="refreshPage">刷新本页</el-button>
    </div>
    <el-alert v-if="availabilityError" :title="availabilityError" type="warning" :closable="false" show-icon />
    <template v-else-if="!loading">
      <section v-if="pageKey === 'migration' && canMigrate" class="maintenance-panel migration-panel">
        <div class="maintenance-body">
          <el-alert v-if="sectionError" :title="sectionError" type="error" :closable="false" show-icon />
          <p class="panel-desc">支持 SQLite、MySQL、PostgreSQL 之间迁移。先验证目标连接，再复制并逐表校验数据；旧数据库不会删除。迁移期间暂停写入，成功后必须立即重启后端。</p>
          <el-alert v-if="status.pendingRestart?.length" title="当前配置等待重启，请先重启后端再发起新的迁移。" type="warning" :closable="false" show-icon />
          <el-alert v-if="migrationError" :title="migrationError" type="error" :closable="false" show-icon />
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
            <el-form-item label="目标数据库类型">
              <el-radio-group :model-value="migrationTarget.type" :disabled="!!working || !!sectionError" @update:model-value="changeMigrationType">
                <el-radio-button label="sqlite">SQLite</el-radio-button>
                <el-radio-button label="mysql">MySQL</el-radio-button>
                <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <div v-if="migrationTarget.type !== 'sqlite'" class="migration-connection-grid">
              <el-form-item label="数据库主机" required>
                <el-input v-model="migrationTarget.host" placeholder="例如 127.0.0.1 或数据库域名" :disabled="!!working" maxlength="253" @update:model-value="invalidateMigration" />
              </el-form-item>
              <el-form-item label="数据库端口" required>
                <el-input-number v-model="migrationTarget.port" :min="1" :max="65535" :precision="0" :disabled="!!working" @update:model-value="invalidateMigration" />
              </el-form-item>
              <el-form-item label="数据库用户名" required>
                <el-input v-model="migrationTarget.username" autocomplete="off" placeholder="具有创建数据库权限的账号" :disabled="!!working" maxlength="128" @update:model-value="invalidateMigration" />
              </el-form-item>
              <el-form-item label="数据库密码" required>
                <el-input v-model="migrationTarget.password" type="password" autocomplete="new-password" placeholder="填写目标连接密码，不会回显旧密码" :disabled="!!working" maxlength="4096" @update:model-value="invalidateMigration" />
              </el-form-item>
            </div>
            <el-form-item :label="migrationTarget.type === 'sqlite' ? '目标数据库文件名' : '目标数据库名称'" required>
              <el-input
                v-model="migrationTargetName"
                :placeholder="migrationTarget.type === 'sqlite' ? '例如 smail-next.db' : '例如 smail_next'"
                :disabled="!!working || !!sectionError"
                maxlength="128"
                clearable
                @update:model-value="invalidateMigration"
              />
            </el-form-item>
            <p class="panel-desc">{{ migrationTarget.type === 'sqlite' ? '文件固定存放在运行目录 config 下，只填写新文件名，不填写路径。' : '请使用尚不存在的库名，系统执行时自动建库。新连接必须填写密码，不会复用源数据库密码。' }}</p>
            <p v-if="migrationTarget.type === 'postgresql'" class="panel-desc">PostgreSQL 连接要求数据库服务启用 TLS 加密，与迁移后的运行连接保持一致。</p>
          </el-form>
          <div class="action-row">
            <el-button :loading="working === 'migration-preview'" :disabled="!!working || !!sectionError || !migrationReady" @click="previewDatabaseMigration">
              验证连接并预览
            </el-button>
            <el-button type="primary" :loading="working === 'migration-execute'" :disabled="!!working || !migrationPreview || !migrationToken" @click="executeDatabaseMigration">
              执行迁移
            </el-button>
            <el-button v-if="migrationRecord?.id" :disabled="!!working" :loading="working === 'migration-status'" @click="loadMigrationStatus">
              刷新迁移状态
            </el-button>
          </div>
          <div v-if="migrationRecord" class="preview-box migration-result">
            <div class="migration-result-heading">
              <strong>迁移状态</strong>
              <el-tag :type="migrationStatusType(migrationRecord.status)">{{ migrationStatusLabel(migrationRecord.status) }}</el-tag>
            </div>
            <span>迁移编号：{{ migrationRecord.id }}</span>
            <span>源数据库：{{ migrationRecord.source?.name || migrationRecord.source?.localRef || '-' }}</span>
            <span>目标数据库：{{ databaseTypeLabel(migrationRecord.target?.type) }} · {{ migrationRecord.target?.name || migrationRecord.target?.localRef || '-' }}</span>
            <span v-if="migrationRecord.target?.host">目标连接：{{ migrationRecord.target.host }}:{{ migrationRecord.target.port }} · {{ migrationRecord.target.username }}</span>
            <el-alert v-if="migrationRecord.status === 'succeeded'" title="迁移完成，旧库已暂停写入。请立即重启后端启用新数据库；若网页提示近期认证已过期，请在部署端重启，不要重复迁移。" type="warning" :closable="false" show-icon />
            <span v-if="migrationRecord.errorCode" class="migration-error">错误代码：{{ migrationRecord.errorCode }}</span>
          </div>
        </div>
      </section>

      <section v-else-if="pageKey === 'restart' && canRestart" class="maintenance-panel restart-panel">
        <div class="maintenance-body">
          <p class="panel-desc">配置变更需要重启时，只能通过固定的 Smail 服务管理器提交请求，不接受外部命令、服务名或参数。</p>
          <el-empty v-if="!status.pendingRestart?.length" description="当前没有待重启服务" :image-size="88" />
          <template v-else>
            <div class="tag-list restart-services">
              <el-tag v-for="service in status.pendingRestart" :key="service" type="warning">{{ serviceLabel(service) }}</el-tag>
            </div>
            <div class="action-row">
              <el-button :disabled="!!working" :loading="working === 'restart-preview'" @click="previewRestart">查看重启预览</el-button>
              <el-button type="danger" :loading="working === 'restart'" :disabled="!!working || !restartPreview?.triggerAvailable" @click="requestRestart">
                提交重启请求
              </el-button>
            </div>
            <div v-if="restartPreview" class="preview-box restart-result">
              <strong>重启预览</strong>
              <span>目标：{{ restartPreview.target }} · 操作：{{ restartPreview.action }}</span>
              <span>服务管理器：{{ restartPreview.serviceManager }}</span>
              <span>{{ restartPreview.triggerAvailable ? '重启通道可用' : '重启通道暂不可用，请先检查服务部署' }}</span>
            </div>
          </template>
        </div>
      </section>

      <section v-else-if="pageKey === 'backups' && canRollback" class="maintenance-panel rollback-panel">
        <div class="maintenance-body">
          <div class="panel-heading">
            <p class="heading-note">选择备份并预览回滚范围，确认后再执行。</p>
            <el-button size="small" :disabled="!!working" :loading="backupsLoading" @click="loadBackups">刷新备份</el-button>
          </div>
          <el-alert v-if="backupsError" :title="backupsError" type="warning" :closable="false" show-icon />
          <el-empty v-else-if="!backups.length && !backupsLoading" description="暂无可用配置备份" :image-size="72" />
          <div v-else class="backup-list">
            <button
              v-for="{ entry: backup } in backupRows"
              :key="backup.generation"
              type="button"
              class="backup-item"
              :disabled="!!working || backupsLoading"
              :class="{ selected: rollbackGeneration === backup.generation }"
              :aria-pressed="rollbackGeneration === backup.generation"
              @click="selectBackup(backup.generation)"
            >
              <span>代次 {{ backup.generation }}</span>
              <small>{{ formatTime(backup.createdAt) }}</small>
            </button>
          </div>
          <ConfigPagination v-model:current-page="backupPage" :page-count="backupPageCount" :total="backups.length" label="备份列表" />
          <el-checkbox v-model="rollbackConfirmed" :disabled="!!working || !rollbackPreview" class="confirmation-check">
            我已阅读回滚预览，并确认执行一次性回滚
          </el-checkbox>
          <div class="action-row">
            <el-button :loading="working === 'preview'" :disabled="!!working || !rollbackGeneration" @click="previewRollback">预览回滚</el-button>
            <el-button type="danger" plain :loading="working === 'rollback'" :disabled="!!working || !rollbackPreview || !rollbackConfirmed" @click="executeRollback">执行回滚</el-button>
          </div>
          <div v-if="rollbackPreview" class="preview-box">
            <strong>回滚预览</strong>
            <span>当前代次 {{ rollbackPreview.currentGeneration }} → 目标代次 {{ rollbackPreview.targetGeneration }}</span>
            <span>候选代次：{{ rollbackPreview.candidateGeneration }}</span>
            <span>变更分区：{{ rollbackPreview.changedSections?.map(sectionLabel).join('、') || '无' }}</span>
          </div>
        </div>
      </section>

      <section v-else-if="pageKey === 'audit' && canAudit" class="maintenance-panel audit-panel">
        <div class="maintenance-body">
          <div class="panel-heading">
            <p class="heading-note">展示最近 50 条，导出最多 500 条记录。</p>
            <div class="audit-actions">
              <el-button size="small" :disabled="!!working" :loading="auditLoading" @click="loadAudit">查询</el-button>
              <el-button v-if="canExportAudit" size="small" type="primary" plain :disabled="!!working" :loading="working === 'export'" @click="exportAudit('json')">导出 JSON</el-button>
              <el-button v-if="canExportAudit" size="small" plain :disabled="!!working" :loading="working === 'export-csv'" @click="exportAudit('csv')">导出 CSV</el-button>
            </div>
          </div>
          <el-alert v-if="auditError" :title="auditError" type="warning" :closable="false" show-icon />
          <div v-else-if="auditItems.length" class="audit-list">
            <div v-for="{ entry: item } in auditRows" :key="item.id || `${item.createdAt}-${item.operation}`" class="audit-item">
              <div>
                <strong>{{ operationLabel(item.operation) }}</strong>
                <span>{{ sectionLabel(item.section) }} · {{ item.actorId || '系统' }}</span>
              </div>
              <time>{{ formatTime(item.createdAt) }}</time>
            </div>
          </div>
          <el-empty v-else-if="!auditLoading" description="暂无配置审计记录" :image-size="72" />
          <ConfigPagination v-model:current-page="auditPage" :page-count="auditPageCount" :total="auditItems.length" label="审计记录" />
        </div>
      </section>
      <el-empty v-else description="当前账号无权访问此页面" />
    </template>
  </div>
</template>

<script setup>
import { useMaintenance } from './ConfigMaintenance.js'
import ConfigPagination from '../ConfigPagination/ConfigPagination.vue'

const props = defineProps({
  pageKey: { type: String, required: true },
  pageTitle: { type: String, required: true }
})
const {
  loading, availabilityError, working, status, refreshPage, beforeLeave, sectionError,
  canMigrate, databaseConfig, databaseTypeLabel, migrationTargetName, migrationPreview,
  migrationToken, migrationRecord, migrationStatusType, migrationStatusLabel,
  migrationTarget, migrationReady, migrationError, invalidateMigration, changeMigrationType,
  previewDatabaseMigration, executeDatabaseMigration, loadMigrationStatus,
  canRestart, serviceLabel, restartPreview, previewRestart, requestRestart,
  canRollback, backupsLoading, backupsError, backups, rollbackGeneration, rollbackConfirmed,
  rollbackPreview, loadBackups, selectBackup, previewRollback, executeRollback,
  canAudit, canExportAudit, auditLoading, auditError, auditItems, loadAudit, exportAudit,
  formatTime, sectionLabel, operationLabel, auditPagination, backupPagination
} = useMaintenance(props.pageKey)
const { currentPage: auditPage, pageCount: auditPageCount, pageItems: auditRows } = auditPagination
const { currentPage: backupPage, pageCount: backupPageCount, pageItems: backupRows } = backupPagination

defineExpose({ beforeLeave })
</script>

<style scoped src="../ConfigSettings.css"></style>
