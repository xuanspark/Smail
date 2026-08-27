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
        <el-button type="primary" class="compose-btn" @click="router.push('/editer')">
          <el-icon><EditPen /></el-icon> {{ lang.compose }}
        </el-button>
      </div>
    </div>

    <div class="list-content">
      <el-table height="100%"
                ref="taskTableDataRef"
                :data="data"
                :show-header="false"
                class="modern-mail-table"
                @row-click="rowClick"
                @selection-change="selectionChange"
                :row-style="rowStyle"
      >
        <el-table-column type="selection" width="40"/>

        <el-table-column width="30" class-name="status-col">
          <template #default="scope">
            <div class="status-indicator">
              <el-badge is-dot class="unread-dot" type="primary" v-if="!scope.row.is_read"></el-badge>
              <el-tooltip effect="dark" :content="lang.dangerous" placement="top-start" v-if="scope.row.dangerous">
                <el-icon color="#ef4444"><Warning /></el-icon>
              </el-tooltip>
              <el-tooltip effect="dark" :content="scope.row.error" placement="top-start" v-if="scope.row.error !== ''">
                <el-icon color="#ef4444"><Warning /></el-icon>
              </el-tooltip>
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
                <div class="mail-subject">{{ scope.row.title }}</div>
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
          @current-change="pageChange"
      />
    </div>
  </div>
</template>

<script setup>
import { usePage, CircleCheck, Switch, Delete, View, Folder, EpArrowDownBold, EditPen, Warning } from './List.js'

const {
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
  pageChange
} = usePage()
</script>

<style scoped src="./List.css"></style>
