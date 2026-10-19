<template>
  <div class="log-subpage-container">
    <div class="log-filters">
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        format="YYYY-MM-DD HH:mm"
        aria-label="操作记录时间范围"
      />
      <el-input
        v-if="canFilterActor"
        v-model="actorId"
        clearable
        placeholder="操作者编号"
        aria-label="操作者编号"
        @keyup.enter="search"
      />
      <div class="log-filter-actions">
        <el-button :disabled="loading" @click="recent">最近 24 小时</el-button>
        <el-button type="primary" :loading="loading" @click="search">查询</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />

    <div v-loading="loading" class="log-content" :aria-busy="loading">
      <div class="log-table-heading">
        <div class="log-table-title">
          <strong>操作记录</strong>
          <span class="log-file-meta">最新在前 · 每页 10 条</span>
        </div>
      </div>

      <el-table
        :data="items"
        class="log-table"
        height="100%"
        row-key="id"
        stripe
        empty-text="暂无操作记录"
        @row-click="item => detail = item"
      >
        <el-table-column label="时间（本地）" width="116">
          <template #default="{ row }">
            <div class="log-time">
              <strong>{{ timeParts(row.createdAt).time }}</strong>
              <span>{{ timeParts(row.createdAt).date }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作与结果说明" min-width="170">
          <template #default="{ row }">
            <strong class="log-action">{{ actionLabel(row.action) }}</strong>
            <div class="log-message">{{ displayOutcome(row) }}</div>
            <div class="log-source">{{ displayObject(row) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="操作者" min-width="85" show-overflow-tooltip>
          <template #default="{ row }">{{ displayActor(row) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="82" align="center">
          <template #default="{ row }">
            <el-tag :type="resultType(row.result)" size="small" class="log-level-tag">
              {{ resultLabel(row.result) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="详情" width="60" align="center">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="detail = row">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <footer class="log-pagination">
      <span aria-live="polite">{{ pageRange }}</span>
      <div class="log-page-actions">
        <el-button size="small" :disabled="loading || offset === 0" @click="previous">上一页</el-button>
        <el-button size="small" :disabled="loading || !hasMore" @click="next">下一页</el-button>
      </div>
    </footer>

    <el-dialog
      :model-value="Boolean(detail)"
      title="操作记录详情"
      width="min(760px, 92vw)"
      append-to-body
      @close="detail = null"
    >
      <template v-if="detail">
        <p>{{ formatTime(detail.createdAt) }} · {{ displayActor(detail) }} · {{ actionLabel(detail.action) }}</p>
        <dl class="log-detail-grid">
          <dt>触发操作</dt>
          <dd>{{ actionLabel(detail.action) }}</dd>
          <dt>触发来源</dt>
          <dd>{{ details.trigger || '未记录' }}</dd>
          <dt>操作对象</dt>
          <dd>{{ displayObject(detail) }}</dd>
          <dt>操作结果</dt>
          <dd>{{ resultLabel(detail.result) }}</dd>
          <dt>结果说明</dt>
          <dd>{{ displayOutcome(detail) }}</dd>
          <dt>权限角色</dt>
          <dd>{{ details.actorRole || '—' }}</dd>
          <dt>结果代码</dt>
          <dd>{{ details.errorCode ?? '—' }}</dd>
          <dt>HTTP 状态</dt>
          <dd>{{ details.httpStatus || '未记录' }}</dd>
        </dl>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { useOperationLogs } from './OperationLogs.js'

const {
  loading,
  error,
  items,
  offset,
  hasMore,
  actorId,
  canFilterActor,
  dateRange,
  detail,
  details,
  pageRange,
  resultType,
  timeParts,
  formatTime,
  resultLabel,
  actionLabel,
  displayActor,
  displayObject,
  displayOutcome,
  search,
  recent,
  next,
  previous
} = useOperationLogs()
</script>

<style scoped src="./OperationLogs.css"></style>