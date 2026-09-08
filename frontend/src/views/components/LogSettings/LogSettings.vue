<template>
  <section class="log-settings">
    <header class="log-heading">
      <h2>日志记录</h2>
      <span class="log-retention">保留 30 天</span>
    </header>
    <el-radio-group v-model="category" class="log-categories" aria-label="日志分类">
      <el-radio-button v-for="item in categories" :key="item.value" :value="item.value">
        {{ item.label }}
      </el-radio-button>
    </el-radio-group>
    <div class="log-filters">
      <el-date-picker
        v-model="dateRange"
        type="datetimerange"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        format="YYYY-MM-DD HH:mm"
        aria-label="日志时间范围"
      />
      <el-input v-if="!isRuntime" v-model="actorId" clearable placeholder="操作者编号" aria-label="操作者编号" @keyup.enter="search" />
      <div class="log-filter-actions">
        <el-button :disabled="loading" @click="recent">最近 24 小时</el-button>
        <el-button type="primary" :loading="loading" @click="search">查询</el-button>
      </div>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div v-loading="loading" class="log-content" :aria-busy="loading">
      <div class="log-table-heading">
        <div class="log-table-title">
          <strong>{{ currentCategory }}</strong>
          <span class="log-file-meta">最新在前 · 每页 10 条</span>
        </div>
        <div v-if="isRuntime" class="log-summary" aria-label="本页日志统计">
          <span>本页 {{ runtimeSummary.total }} 条</span>
          <span class="log-stat-info"><i aria-hidden="true" />信息 {{ runtimeSummary.info }}</span>
          <span class="log-stat-warning"><i aria-hidden="true" />警告 {{ runtimeSummary.warning }}</span>
          <span class="log-stat-danger"><i aria-hidden="true" />错误 {{ runtimeSummary.error }}</span>
        </div>
      </div>
      <el-table
        v-if="isRuntime"
        :data="runtimeRows"
        class="log-table runtime-table"
        height="100%"
        row-key="sequence"
        :row-class-name="runtimeRowClass"
        empty-text="暂无运行日志"
        @row-click="row => runtimeDetail = row"
      >
        <el-table-column label="时间（本地）" width="116">
          <template #default="{ row }">
            <div class="log-time"><strong>{{ row.time }}</strong><span>{{ row.date }}</span></div>
          </template>
        </el-table-column>
        <el-table-column label="级别" width="76" align="center">
          <template #default="{ row }">
            <el-tag :type="row.type" size="small" effect="light" class="log-level-tag">{{ row.label }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="日志内容" min-width="170">
          <template #default="{ row }">
            <div class="log-message">{{ row.summary }}</div>
            <div v-if="row.sourceShort" class="log-source">{{ row.sourceShort }}</div>
          </template>
        </el-table-column>
        <el-table-column label="详情" width="60" align="center">
          <template #default="{ row }">
            <el-button link type="primary" :aria-label="`查看第 ${row.sequence} 条日志详情`" @click.stop="runtimeDetail = row">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-table v-else :data="items" class="log-table" height="100%" row-key="id" stripe empty-text="暂无日志记录" @row-click="item => detail = item">
        <el-table-column label="时间（本地）" width="116">
          <template #default="{ row }">
            <div class="log-time"><strong>{{ timeParts(row.createdAt).time }}</strong><span>{{ timeParts(row.createdAt).date }}</span></div>
          </template>
        </el-table-column>
        <el-table-column label="操作" min-width="125" show-overflow-tooltip>
          <template #default="{ row }">{{ actionLabel(row.action) }}</template>
        </el-table-column>
        <el-table-column label="操作者" min-width="85" show-overflow-tooltip>
          <template #default="{ row }">{{ displayActor(row) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="82" align="center">
          <template #default="{ row }">
            <el-tag :type="resultType(row.result)" size="small" class="log-level-tag">{{ resultLabel(row.result) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="详情" width="60" align="center">
          <template #default="{ row }"><el-button link type="primary" @click.stop="detail = row">查看</el-button></template>
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
      :model-value="Boolean(runtimeDetail)"
      title="运行日志详情"
      width="min(760px, 92vw)"
      append-to-body
      @close="runtimeDetail = null"
    >
      <div v-if="runtimeDetail" class="runtime-detail">
        <div class="runtime-detail-heading">
          <el-tag :type="runtimeDetail.type" effect="light">{{ runtimeDetail.label }}</el-tag>
          <span>{{ formatTime(runtimeDetail.createdAt) }}</span>
          <span class="runtime-detail-sequence">第 {{ runtimeDetail.sequence }} 条</span>
        </div>
        <dl class="log-detail-grid runtime-detail-meta">
          <dt>代码来源</dt><dd>{{ runtimeDetail.source || '未记录' }}</dd>
          <template v-if="runtimeDetail.requestId">
            <dt>请求标识</dt><dd>{{ runtimeDetail.requestId }}</dd>
          </template>
        </dl>
        <h3>完整内容</h3>
        <pre class="runtime-detail-message">{{ runtimeDetail.message }}</pre>
        <details class="runtime-raw">
          <summary>原始记录</summary>
          <pre>{{ runtimeDetail.raw }}</pre>
        </details>
      </div>
    </el-dialog>
    <el-dialog :model-value="Boolean(detail)" title="日志详情" width="min(760px, 92vw)" append-to-body @close="detail = null">
      <template v-if="detail">
        <p>{{ formatTime(detail.createdAt) }} · {{ displayActor(detail) }} · {{ actionLabel(detail.action) }}</p>
        <el-table v-if="changes.length" :data="changes" max-height="440">
          <el-table-column prop="field" label="变更字段" min-width="200" />
          <el-table-column prop="before" label="修改前" min-width="150" />
          <el-table-column prop="after" label="修改后" min-width="150" />
        </el-table>
        <p v-else-if="category === 'config'">
          {{ detail.id.startsWith('legacy-') || !details.actorName ? '此记录未保存字段前后值，不补造历史详情。' : '此次保存没有业务字段变更。' }}
        </p>
        <dl v-else-if="category === 'mail'" class="log-detail-grid">
          <dt>发件人</dt><dd>{{ details.sender || '—' }}</dd>
          <dt>收件人数</dt><dd>{{ details.recipientCount }}</dd>
          <dt>邮件大小</dt><dd>{{ details.bytes }} 字节</dd>
          <dt>结果</dt><dd>{{ resultLabel(detail.result) }}</dd>
        </dl>
        <dl v-else class="log-detail-grid">
          <dt>权限角色</dt><dd>{{ details.actorRole || '—' }}</dd>
          <dt>结果代码</dt><dd>{{ details.errorCode ?? '—' }}</dd>
        </dl>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { useLogs } from './LogSettings.js'

const {
  category, categories, loading, error, items, offset,
  hasMore, actorId, dateRange, detail, details, changes, isRuntime,
  runtimeDetail, runtimeRows, runtimeSummary, pageRange,
  currentCategory, resultType, runtimeRowClass, timeParts,
  formatTime, resultLabel, actionLabel, displayActor, search, recent, next, previous
} = useLogs()
</script>

<style scoped src="./LogSettings.css"></style>
