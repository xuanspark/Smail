<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.account_management }}</h3>
      <p class="settings-desc">{{ lang.account_management_desc }}</p>
    </div>

    <div class="table-container">
      <el-table :data="userList" class="modern-table" style="width: 100%" empty-text="暂无用户">
        <el-table-column label="ID" prop="ID" width="44" />
        <el-table-column :label="lang.account" prop="Account" width="88" show-overflow-tooltip />
        <el-table-column :label="lang.user_name" prop="Name" width="76" show-overflow-tooltip />
        <el-table-column label="权限级别" width="100">
          <template #default="scope">
            <el-tag
              :type="roleMeta(scope.row.Role).type"
              size="small"
              effect="light"
              class="status-tag role-tag"
            >
              {{ roleMeta(scope.row.Role).label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="安全码" width="72">
          <template #default="scope">
            <el-tag
              :type="recoveryCodeMeta(scope.row.RecoveryCodeConfigured).type"
              size="small"
              effect="plain"
              class="status-tag"
            >
              {{ recoveryCodeMeta(scope.row.RecoveryCodeConfigured).label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="外部邮箱" width="76">
          <template #default="scope">
            <el-tag
              :type="externalEmailMeta(scope.row.ExternalEmailEnabled, scope.row.ExternalEmailConfigured).type"
              size="small"
              effect="plain"
              class="status-tag"
            >
              {{ externalEmailMeta(scope.row.ExternalEmailEnabled, scope.row.ExternalEmailConfigured).label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="账号状态" width="72">
          <template #default="scope">
            <el-tag
              :type="accountStatusMeta(scope.row.Disabled).type"
              size="small"
              effect="plain"
              class="status-tag"
            >
              {{ accountStatusMeta(scope.row.Disabled).label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column align="right" width="68">
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
        size="small"
        background
        layout="prev, pager, next"
        :page-count="totalPage"
        @current-change="reflushList"
      />
    </div>

    <el-dialog
      v-model="userInfoDialog"
      :title="title"
      width="450px"
      class="premium-dialog"
      :close-on-click-modal="!submitting"
      :close-on-press-escape="!submitting"
      :show-close="!submitting"
    >
      <div class="dialog-content">
        <el-form label-position="top">
          <el-form-item :label="lang.account">
            <el-input :disabled="editModel === 'edit' || submitting" v-model="editUserInfo.account" />
          </el-form-item>

          <el-form-item :label="lang.user_name">
            <el-input v-model="editUserInfo.name" :disabled="!canEditSelectedUser || submitting" />
          </el-form-item>

          <el-form-item :label="lang.password">
            <el-input
              :placeholder="editModel === 'create' ? '请输入 8–12 字节密码' : '留空则不修改密码'"
              v-model="editUserInfo.password"
              :disabled="!canEditSelectedUser || submitting"
              type="password"
              show-password
            />
          </el-form-item>

          <el-form-item label="权限级别">
            <el-select
              v-if="canSelectRole"
              v-model="editUserInfo.role"
              aria-label="权限级别"
              class="role-select"
              :disabled="submitting || availableRoles.length < 2"
            >
              <el-option
                v-for="option in availableRoles"
                :key="option.value"
                :label="option.label"
                :value="option.value"
              />
            </el-select>
            <el-tag v-else :type="roleMeta(editUserInfo.role).type">
              {{ roleMeta(editUserInfo.role).label }}
            </el-tag>
            <span v-if="roleChanged && !selectedUser?.RecoveryCodeConfigured" class="role-requirement">
              更改级别前，请先在下方配置该用户的安全码。
            </span>
          </el-form-item>

          <div v-if="editModel === 'edit'" class="security-edit-grid">
            <div class="security-edit-item">
              <div>
                <strong>安全码</strong>
                <span v-if="recoveryCodeWebManaged">
                  {{ recoveryCodeMeta(selectedUser?.RecoveryCodeConfigured).label }}
                </span>
                <span v-else>部署端管理</span>
              </div>
              <el-button
                :disabled="!recoveryCodeWebManaged || submitting"
                :loading="securityDialogOpening === 'recovery-code'"
                @click="openRecoveryCodeDialog"
              >
                {{ recoveryCodeWebManaged ? '管理安全码' : '仅支持本机管理' }}
              </el-button>
            </div>
            <div class="security-edit-item">
              <div>
                <strong>外部邮箱</strong>
                <span>
                  {{ externalEmailMeta(
                    selectedUser?.ExternalEmailEnabled,
                    selectedUser?.ExternalEmailConfigured
                  ).label }}
                </span>
              </div>
              <el-button
                :disabled="submitting"
                :loading="securityDialogOpening === 'external-email'"
                @click="openExternalEmailDialog"
              >
                编辑外部邮箱
              </el-button>
            </div>
          </div>

          <el-form-item>
            <div class="status-switch">
              <span class="switch-label">{{ lang.status }}</span>
              <el-switch
                v-model="editUserInfo.disabled"
                :disabled="!canEditSelectedUser || submitting"
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
          <el-button :disabled="submitting" @click="userInfoDialog = false">{{ lang.cancel }}</el-button>
          <el-button type="primary" :disabled="!canEditSelectedUser" :loading="submitting" @click="submit">
            {{ lang.confirm }}
          </el-button>
        </span>
      </template>
    </el-dialog>

    <RecoveryCodeDialog
      v-model="recoveryCodeDialogVisible"
      :user="selectedUser"
      @changed="handleSecurityChanged"
    />
    <UserExternalEmailDialog
      v-model="externalEmailDialogVisible"
      :user="selectedUser"
      :self-managed="editingCurrentUser"
      @changed="handleSecurityChanged"
    />
  </div>
</template>

<script setup>
import { usePage, Plus } from "./UserManagement.js";
import RecoveryCodeDialog from "./RecoveryCodeDialog/RecoveryCodeDialog.vue";
import UserExternalEmailDialog from "./UserExternalEmailDialog/UserExternalEmailDialog.vue";

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
  editUserInfo,
  availableRoles,
  canEditSelectedUser,
  canSelectRole,
  roleChanged,
  submitting,
  selectedUser,
  editingCurrentUser,
  recoveryCodeWebManaged,
  recoveryCodeDialogVisible,
  externalEmailDialogVisible,
  securityDialogOpening,
  openRecoveryCodeDialog,
  openExternalEmailDialog,
  handleSecurityChanged,
  roleMeta,
  recoveryCodeMeta,
  externalEmailMeta,
  accountStatusMeta
} = usePage();
</script>

<style scoped src="./UserManagement.css"></style>
