<template>
  <section class="log-settings">
    <header class="log-heading">
      <h2>日志</h2>
      <span>保留最近 30 天 · 运行日志满 7 天压缩</span>
    </header>
    <el-radio-group v-model="category" class="log-categories" aria-label="日志分类">
      <el-radio-button v-for="item in categories" :key="item.value" :value="item.value">
        {{ item.label }}
      </el-radio-button>
    </el-radio-group>
    <div class="log-filters">
      <template v-if="isRuntime">
        <el-select v-model="selectedFile" placeholder="选择日志文件" aria-label="日志文件" @change="search">
          <el-option
            v-for="file in files"
            :key="file.name"
            :value="file.name"
            :label="`${file.name}${file.compressed ? '（已压缩）' : ''}`"
          />
        </el-select>
      </template>
      <template v-else>
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          format="YYYY-MM-DD HH:mm"
          aria-label="日志时间范围"
        />
        <el-input v-model="actorId" clearable placeholder="操作者编号" aria-label="操作者编号" @keyup.enter="search" />
      </template>
      <el-button type="primary" :loading="loading" @click="search">查询</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <div v-loading="loading" class="log-content">
      <template v-if="isRuntime">
        <pre v-if="lines.length" class="runtime-lines">{{ lines.join('\n') }}</pre>
        <el-empty v-else description="暂无运行日志" />
      </template>
      <el-table v-else :data="items" height="100%" empty-text="暂无日志记录" @row-click="item => detail = item">
        <el-table-column label="时间" min-width="165">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" min-width="140">
          <template #default="{ row }">{{ actionLabel(row.action) }}</template>
        </el-table-column>
        <el-table-column label="操作者" min-width="110">
          <template #default="{ row }">{{ displayActor(row) }}</template>
        </el-table-column>
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag :type="row.result === 'failure' ? 'danger' : 'info'" size="small">{{ resultLabel(row.result) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="详情" width="72">
          <template #default="{ row }"><el-button link type="primary" @click.stop="detail = row">查看</el-button></template>
        </el-table-column>
      </el-table>
    </div>
    <footer class="log-pagination">
      <span>从第 {{ offset + 1 }} 条开始</span>
      <el-button :disabled="loading || offset === 0" @click="previous">上一页</el-button>
      <el-button :disabled="loading || !hasMore" @click="next">下一页</el-button>
    </footer>
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
  category, categories, loading, error, items, files, selectedFile, lines, offset,
  hasMore, actorId, dateRange, detail, details, changes, isRuntime,
  formatTime, resultLabel, actionLabel, displayActor, search, next, previous
} = useLogs()
</script>

<style scoped src="./LogSettings.css"></style>
