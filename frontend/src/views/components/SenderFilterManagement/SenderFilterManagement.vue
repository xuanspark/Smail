<template>
  <div class="settings-card sender-filter-card" v-loading="loading">
    <div class="settings-header">
      <div class="header-main">
        <h3>发件人黑白名单</h3>
        <p class="settings-desc">
          管理受信任的白名单与受拦截的黑名单发件人。白名单邮件不提示风险，黑名单发件人邮件将强行标记风险警示。
        </p>
      </div>
      <div class="sf-badges">
        <el-tag type="success" effect="plain" class="sf-badge">
          信任白名单: {{ whitelistCount }}
        </el-tag>
        <el-tag type="danger" effect="plain" class="sf-badge">
          拦截黑名单: {{ blacklistCount }}
        </el-tag>
      </div>
    </div>

    <!-- 快捷添加区域 -->
    <div class="sf-quick-add-box">
      <el-input
        v-model="addForm.email"
        placeholder="输入发件人邮箱 (如 friend@example.com)"
        clearable
        size="default"
        class="sf-input-email"
        @keyup.enter="handleAdd"
      />
      <el-select
        v-model="addForm.filter_type"
        size="default"
        class="sf-select-type"
      >
        <el-option label="信任白名单" value="whitelist" />
        <el-option label="拦截黑名单" value="blacklist" />
      </el-select>
      <el-button
        type="primary"
        size="default"
        :loading="addLoading"
        @click="handleAdd"
      >
        添加名单
      </el-button>
    </div>

    <!-- 筛选与搜索工具条 -->
    <div class="sf-toolbar">
      <el-radio-group
        v-model="currentTab"
        size="small"
        class="sf-tabs"
        @change="handleTabChange"
      >
        <el-radio-button value="all">全部 ({{ total }})</el-radio-button>
        <el-radio-button value="whitelist">仅白名单 ({{ whitelistCount }})</el-radio-button>
        <el-radio-button value="blacklist">仅黑名单 ({{ blacklistCount }})</el-radio-button>
      </el-radio-group>
      <div class="sf-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索邮箱地址..."
          clearable
          size="small"
          class="sf-search-input"
          @clear="handleSearch"
          @keyup.enter="handleSearch"
        >
          <template #append>
            <el-button size="small" @click="handleSearch">搜索</el-button>
          </template>
        </el-input>
      </div>
    </div>

    <!-- 规则表格 -->
    <div class="sf-table-wrapper">
      <el-table
        :data="filterList"
        stripe
        size="default"
        class="sf-table"
        empty-text="暂无匹配的发件人名单"
      >
        <el-table-column prop="email" label="发件人邮箱" min-width="220" show-overflow-tooltip />
        <el-table-column label="名单属性" width="130" align="center">
          <template #default="{ row }">
            <el-tag
              v-if="row.filter_type === 'whitelist'"
              type="success"
              size="small"
              effect="light"
            >
              信任白名单
            </el-tag>
            <el-tag
              v-else
              type="danger"
              size="small"
              effect="light"
            >
              拦截黑名单
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="160" align="center">
          <template #default="{ row }">
            <span>{{ formatDate(row.updated_at || row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="管理操作" width="180" align="center">
          <template #default="{ row }">
            <div class="sf-actions">
              <el-button
                v-if="row.filter_type === 'whitelist'"
                type="warning"
                link
                size="small"
                @click="handleToggle(row)"
              >
                拉入黑名单
              </el-button>
              <el-button
                v-else
                type="success"
                link
                size="small"
                @click="handleToggle(row)"
              >
                转为白名单
              </el-button>
              <el-button
                type="danger"
                link
                size="small"
                @click="handleDelete(row)"
              >
                移除
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="sf-pagination">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        size="small"
        @current-change="handlePageChange"
      />
    </div>
  </div>
</template>

<script setup>
import { useSenderFilters } from './SenderFilterManagement.js'
import './SenderFilterManagement.css'

const {
  loading,
  filterList,
  total,
  whitelistCount,
  blacklistCount,
  currentPage,
  pageSize,
  searchKeyword,
  currentTab,
  addForm,
  addLoading,
  handleSearch,
  handleTabChange,
  handlePageChange,
  handleAdd,
  handleToggle,
  handleDelete,
  formatDate,
} = useSenderFilters()
</script>