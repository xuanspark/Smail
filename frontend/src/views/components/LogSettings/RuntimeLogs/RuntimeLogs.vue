<template>
  <div class="log-subpage-container">
    <div class="log-filters">
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        start-placeholder="????"
        end-placeholder="????"
        format="YYYY-MM-DD HH:mm"
        aria-label="????????"
      />
      <div class="log-filter-actions">
        <el-button :disabled="loading" @click="recent">?? 24 ??</el-button>
        <el-button type="primary" :loading="loading" @click="search">??</el-button>
      </div>
    </div>

    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />

    <div v-loading="loading" class="log-content" :aria-busy="loading">
      <div class="log-table-heading">
        <div class="log-table-title">
          <strong>????</strong>
          <span class="log-file-meta">???? ? ?? 10 ?</span>
        </div>
        <div class="log-summary" aria-label="??????">
          <span>?? {{ runtimeSummary.total }} ?</span>
          <span class="log-stat-info"><i aria-hidden="true" />?? {{ runtimeSummary.info }}</span>
          <span class="log-stat-warning"><i aria-hidden="true" />?? {{ runtimeSummary.warning }}</span>
          <span class="log-stat-danger"><i aria-hidden="true" />?? {{ runtimeSummary.error }}</span>
        </div>
      </div>

      <el-table
        :data="runtimeRows"
        class="log-table runtime-table"
        height="100%"
        row-key="sequence"
        :row-class-name="runtimeRowClass"
        empty-text="??????"
        @row-click="row => runtimeDetail = row"
      >
        <el-table-column label="??????" width="116">
          <template #default="{ row }">
            <div class="log-time">
              <strong>{{ row.time }}</strong>
              <span>{{ row.date }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="??" width="76" align="center">
          <template #default="{ row }">
            <el-tag :type="row.type" size="small" effect="light" class="log-level-tag">
              {{ row.label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="???????" min-width="170">
          <template #default="{ row }">
            <strong class="log-action">{{ row.action }}</strong>
            <div class="log-message">{{ row.outcome }}</div>
            <div v-if="row.object" class="log-source">???{{ row.object }}</div>
          </template>
        </el-table-column>
        <el-table-column label="??" width="92" align="center">
          <template #default="{ row }">
            <el-tag :type="resultType(row.result)" size="small" class="log-level-tag">
              {{ resultLabel(row.result) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="??" width="60" align="center">
          <template #default="{ row }">
            <el-button link type="primary" :aria-label="'??? ' + row.sequence + ' ?????'" @click.stop="runtimeDetail = row">
              ??
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <footer class="log-pagination">
      <span aria-live="polite">{{ pageRange }}</span>
      <div class="log-page-actions">
        <el-button size="small" :disabled="loading || offset === 0" @click="previous">???</el-button>
        <el-button size="small" :disabled="loading || !hasMore" @click="next">???</el-button>
      </div>
    </footer>

    <el-dialog
      :model-value="Boolean(runtimeDetail)"
      title="??????"
      width="min(760px, 92vw)"
      append-to-body
      @close="runtimeDetail = null"
    >
      <div v-if="runtimeDetail" class="runtime-detail">
        <div class="runtime-detail-heading">
          <el-tag :type="runtimeDetail.type" effect="light">{{ runtimeDetail.label }}</el-tag>
          <span>{{ formatTime(runtimeDetail.createdAt) }}</span>
          <span class="runtime-detail-sequence">? {{ runtimeDetail.sequence }} ?</span>
        </div>
        <dl class="log-detail-grid runtime-detail-meta">
          <dt>????</dt>
          <dd>{{ runtimeDetail.action }}</dd>
          <dt>????</dt>
          <dd>{{ runtimeDetail.object || '???' }}</dd>
          <dt>????</dt>
          <dd>{{ resultLabel(runtimeDetail.result) }}</dd>
          <dt>????</dt>
          <dd>{{ runtimeDetail.outcome }}</dd>
          <dt>????</dt>
          <dd>{{ runtimeDetail.source || '???' }}</dd>
          <template v-if="runtimeDetail.requestId">
            <dt>????</dt>
            <dd>{{ runtimeDetail.requestId }}</dd>
          </template>
        </dl>
        <h3>????</h3>
        <pre class="runtime-detail-message">{{ runtimeDetail.message }}</pre>
        <details class="runtime-raw">
          <summary>????</summary>
          <pre>{{ runtimeDetail.raw }}</pre>
        </details>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { useRuntimeLogs } from './RuntimeLogs.js'

const {
  loading,
  error,
  items,
  offset,
  hasMore,
  dateRange,
  runtimeDetail,
  runtimeRows,
  runtimeSummary,
  pageRange,
  resultType,
  runtimeRowClass,
  formatTime,
  resultLabel,
  search,
  recent,
  next,
  previous
} = useRuntimeLogs()
</script>

<style scoped src="./RuntimeLogs.css"></style>
