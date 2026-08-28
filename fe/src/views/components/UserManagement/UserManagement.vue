<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.account_management }}</h3>
      <p class="settings-desc">{{ lang.account_management_desc }}</p>
    </div>

    <div class="table-container">
      <el-table :data="userList" class="modern-table" style="width: 100%">
        <el-table-column label="ID" prop="ID" width="80" />
        <el-table-column :label="lang.account" prop="Account" min-width="150" show-overflow-tooltip />
        <el-table-column :label="lang.user_name" prop="Name" min-width="120" show-overflow-tooltip />
        <el-table-column :label="lang.disabled" prop="Disabled" width="120">
          <template #default="scope">
            <el-tag
              :type="scope.row.Disabled === 1 ? 'info' : 'success'"
              size="small"
              effect="plain"
              class="status-tag"
            >
              {{ scope.row.Disabled === 1 ? lang.disabled : lang.enabled }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column align="right" width="100">
          <template #header>
            <el-button type="primary" size="small" @click="createUser" class="new-btn" plain>
              <el-icon><Plus /></el-icon> 新建
            </el-button>
          </template>
          <template #default="scope">
            <el-button
              size="small"
              type="primary"
              text
              bg
              @click="handleEdit(scope.$index, scope.row)"
              class="action-btn"
            >
              编辑
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="currentPage"
        small
        background
        layout="prev, pager, next"
        :page-count="totalPage"
        @current-change="reflushList"
      />
    </div>

    <el-dialog v-model="userInfoDialog" :title="title" width="450px" class="premium-dialog">
      <div class="dialog-content">
        <el-form label-position="top">
          <el-form-item :label="lang.account">
            <el-input :disabled="editModel === 'edit'" v-model="editUserInfo.account" />
          </el-form-item>

          <el-form-item :label="lang.user_name">
            <el-input v-model="editUserInfo.name" />
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input
              :placeholder="lang.resetPwd"
              v-model="editUserInfo.password"
              type="password"
              show-password
            />
          </el-form-item>

          <el-form-item>
            <div class="status-switch">
              <span class="switch-label">{{ lang.status }}</span>
              <el-switch
                v-model="editUserInfo.disabled"
                class="ml-2"
                :active-text="lang.disabled"
                :inactive-text="lang.enabled"
                active-color="#ef4444"
                inactive-color="#10b981"
              />
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="userInfoDialog = false">{{ lang.cancel }}</el-button>
          <el-button type="primary" @click="submit">{{ lang.confirm }}</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { usePage, Plus } from "./UserManagement.js";

const {
  lang,
  userList,
  createUser,
  handleEdit,
  currentPage,
  totalPage,
  reflushList,
  userInfoDialog,
  title,
  submit,
  editModel,
  editUserInfo
} = usePage();
</script>

<style scoped src="./UserManagement.css"></style>
