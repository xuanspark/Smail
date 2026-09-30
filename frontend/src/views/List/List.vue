<template>
<div class="list-view-container">
    <div class="list-header">
      <div class="header-title">
        <h2>{{ groupStore.name }}</h2>
      </div>
      <div class="header-actions">
        <div class="selection-actions">
          <el-button @click="selectAll" class="action-btn selection-btn" plain :disabled="data.length === 0">
            <el-icon><CircleCheck /></el-icon> {{ lang.select_all_btn }}
          </el-button>
          <el-button @click="invertSelection" class="action-btn selection-btn" plain :disabled="data.length === 0">
            <el-icon><Switch /></el-icon> {{ lang.invert_selection_btn }}
          </el-button>
        </div>
        <el-button @click="del" class="action-btn" plain>
          <el-icon><Delete /></el-icon> {{ lang.del_btn }}
        </el-button>
        <el-button @click="markRead" class="action-btn" plain>
          <el-icon><View /></el-icon> {{ lang.read_btn }}
        </el-button>
        <el-dropdown class="move-dropdown">
          <el-button class="action-btn" plain>
            <el-icon><Folder /></el-icon> {{ lang.move_btn }}
            <el-icon class="el-icon--right"><EpArrowDownBold/></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="move(group.id,group.name)" v-for="group in groupList" :key="group.id">
                {{ group.name }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button @click="manualRefresh" class="action-btn refresh-btn" :loading="isRefreshing" plain>
          <el-icon><Refresh /></el-icon> {{ lang.refresh }}
        </el-button>
        <el-button type="primary" class="compose-btn" @click="router.push('/editer')">
          <el-icon><EditPen /></el-icon> {{ lang.compose }}
        </el-button>
      </div>
    </div>

    <div class="list-content">
      <el-table height="100%"
                ref="taskTableDataRef"
                :data="data"
                row-key="id"
                :show-header="false"
                class="modern-mail-table"
                @row-click="rowClick"
                @selection-change="selectionChange"
                :row-style="rowStyle"
      >
        <el-table-column type="selection" width="40" :reserve-selection="true"/>

        <el-table-column width="30" class-name="status-col">
          <template #default="scope">
            <div class="status-indicator">
              <el-badge is-dot class="unread-dot" type="primary" v-if="!scope.row.is_read"></el-badge>
            </div>
          </template>
        </el-table-column>

        <el-table-column min-width="250">
          <template #default="scope">
            <div class="mail-row-content" :class="{'is-unread': !scope.row.is_read}">
              <div class="mail-main-info">
                <div class="mail-sender">
                  {{ scope.row.sender.Name !== '' ? scope.row.sender.Name : scope.row.sender.EmailAddress }}
                </div>
                
                <!-- 高辨识度安全与发送状态标签 -->
                <div class="mail-status-tags" v-if="scope.row.dangerous || isOutbox">
                  <el-tag v-if="scope.row.is_blacklisted" type="danger" effect="dark" size="small">
                    黑名单发件人
                  </el-tag>
                  <template v-else>
                  <el-tooltip content="点击将该发件人添加至信任白名单，后续免除风险提示" placement="top" v-if="scope.row.dangerous">
                    <el-tag type="danger" effect="dark" size="small" class="clickable-tag" @click.stop="trustSender(scope.row.sender.EmailAddress)">
                      <el-icon><Warning /></el-icon> 疑似不安全 (点击信任)
                    </el-tag>
                  </el-tooltip>
                  
                  </template>
                  <template v-if="isOutbox">
                    <el-tag type="warning" effect="light" size="small" v-if="scope.row.status === 6">
                      待投递
                    </el-tag>
                    <el-tooltip :content="scope.row.error" placement="top" v-else-if="scope.row.status === 7">
                      <el-tag type="warning" effect="light" size="small" class="clickable-tag" @click.stop="showError(scope.row)">
                        <el-icon><Warning /></el-icon> 投递结果待核对
                      </el-tag>
                    </el-tooltip>
                    <el-tooltip :content="scope.row.error" placement="top" v-else-if="isPartialDelivery(scope.row)">
                      <el-tag type="warning" effect="light" size="small" class="clickable-tag" @click.stop="showError(scope.row)">
                        <el-icon><Warning /></el-icon> 部分成功 (查看详情)
                      </el-tag>
                    </el-tooltip>
                    <el-tag type="success" effect="light" size="small" v-else-if="scope.row.status === 1 && !scope.row.error">
                      <el-icon><Check /></el-icon> 发送成功
                    </el-tag>
                    <el-tooltip :content="scope.row.error || '未知投递错误'" placement="top" v-else-if="scope.row.status === 2 || scope.row.error">
                      <el-tag type="danger" effect="light" size="small" class="clickable-tag" @click.stop="showError(scope.row)">
                        <el-icon><Warning /></el-icon> 发送失败 (查看详情)
                      </el-tag>
                    </el-tooltip>
                    <el-tag type="info" effect="light" size="small" v-else>状态未确认</el-tag>
                  </template>
                </div>

                <div class="mail-subject">{{ scope.row.title || lang.mail_no_subject }}</div>
                <div class="mail-snippet">{{ scope.row.desc }}</div>
              </div>
              <div class="mail-meta">
                <div class="mail-date">{{ formatShortDate(scope.row.datetime) }}</div>
              </div>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-wrapper" v-if="totalPage > 0">
      <el-pagination
          background
          layout="prev, pager, next"
          :page-count="totalPage"
          v-model:current-page="currentPage"
          @current-change="pageChange"
      />
    </div>

    <!-- 错误详情对话框 -->
    <el-dialog v-model="errorDialogVisible" title="邮件投递详情" width="500px">
      <div class="error-dialog-body">
        <p class="error-label">错误原因：</p>
        <el-alert :title="currentErrorMsg" type="error" :closable="false" show-icon />
      </div>
      <template #footer>
        <el-button type="primary" @click="errorDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { usePage, CircleCheck, Switch, Delete, View, Folder, EpArrowDownBold, EditPen, Warning, Check, Refresh } from './List.js'

const {
  taskTableDataRef,
  groupStore,
  selectAll,
  data,
  lang,
  invertSelection,
  del,
  markRead,
  groupList,
  move,
  router,
  rowClick,
  selectionChange,
  rowStyle,
  formatShortDate,
  totalPage,
  currentPage,
  pageChange,
  isOutbox,
  trustSender,
  showError,
  isPartialDelivery,
  errorDialogVisible,
  currentErrorMsg,
  isRefreshing,
  manualRefresh
} = usePage()
</script>

<style scoped src="./List.css"></style>
