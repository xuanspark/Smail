<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.account_management }}</h3>
      <div class="group-toolbar">
        <el-button v-if="canCreateUser" type="primary" :disabled="groupsLoading" @click="createUser">
          <el-icon><Plus /></el-icon> 新建账号
        </el-button>
        <el-button v-if="canManageGroups" :loading="groupSaving" @click="saveGroup()">新建分组</el-button>
        <el-button :loading="groupsLoading" @click="reflushList">刷新列表</el-button>
      </div>
    </div>

    <el-alert v-if="groupsError" :title="groupsError" type="error" :closable="false" />
    <el-empty v-if="!groupsLoading && !groupsError && accountGroups.length === 0" description="暂无可查看的分组" />
    <section v-for="group in accountGroups" :key="group.id" class="account-group-panel">
      <header class="account-group-header">
        <h4 v-if="group.id === 0">{{ group.name }}</h4>
        <button
          v-else
          type="button"
          class="account-group-toggle"
          :aria-expanded="group.expanded"
          :aria-controls="`account-group-${group.id}`"
          @click="toggleGroup(group)"
        >
          <span aria-hidden="true">{{ group.expanded ? '▾' : '▸' }}</span>
          {{ group.name }}
        </button>
        <el-button v-if="canManageGroups && group.id !== 0" text :disabled="groupSaving" @click="saveGroup(group)">重命名</el-button>
      </header>
      <div v-if="group.expanded" :id="`account-group-${group.id}`" v-loading="group.loading">
        <el-alert v-if="group.error" :title="group.error" type="error" :closable="false" />
    <div class="table-container">
      <el-table :data="group.rows" class="modern-table" style="width: 100%" empty-text="本组暂无成员">
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
              :type="externalEmailMeta(scope.row.ExternalEmailConfigured).type"
              size="small"
              effect="plain"
              class="status-tag"
            >
              {{ externalEmailMeta(scope.row.ExternalEmailConfigured).label }}
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
        <el-table-column label="操作" align="right" :width="canManageMembership ? 126 : 68">
          <template #default="scope">
            <el-button
              size="small"
              type="primary"
              text
              bg
              @click="handleEdit(scope.$index, scope.row)"
              :disabled="!canEditUser(scope.row)"
              class="action-btn"
            >
              编辑
            </el-button>
            <el-button v-if="canManageMembership" size="small" text :disabled="groupSaving" @click="editMembership(scope.row)">分组</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="group.page"
        size="small"
        background
        layout="prev, pager, next"
        :page-count="group.totalPages"
        :disabled="group.loading"
        @current-change="loadGroup(group)"
      />
    </div>
      </div>
    </section>

    <el-dialog v-model="membershipVisible" title="设置账号所属分组" width="450px" :close-on-click-modal="!groupSaving" :close-on-press-escape="!groupSaving" :show-close="!groupSaving" append-to-body>
      <p>账号：{{ membershipUser?.Account }}</p>
      <el-form label-position="top">
        <el-form-item label="所属分组">
          <div class="membership-selector-header">
            <span>{{ membershipLimit > 1 ? '可选择多个分组' : '只能选择一个分组' }}</span>
            <el-button
              v-if="canManageGroups"
              type="primary"
              text
              size="small"
              :disabled="groupSaving"
              @click="createMembershipGroup"
            >
              新建分组
            </el-button>
          </div>
          <fieldset
            class="membership-options"
            :aria-label="membershipLimit > 1 ? '所属分组（可多选）' : '所属分组（单选）'"
            :disabled="groupSaving"
          >
            <label
              v-for="group in accountGroups"
              :key="group.id"
              class="membership-option"
            >
              <input
                v-if="membershipLimit > 1"
                v-model="membershipSelection"
                type="checkbox"
                :value="group.id"
                :disabled="membershipSelection.length >= membershipLimit && !membershipSelection.includes(group.id)"
              />
              <input
                v-else
                v-model="membershipSingleSelection"
                type="radio"
                name="account-membership"
                :value="group.id"
              />
              <span>{{ group.name }}</span>
            </label>
          </fieldset>
        </el-form-item>
      </el-form>
      <el-alert title="变更分组后需重新登录并重建通知发信凭据。" type="warning" :closable="false" />
      <template #footer>
        <el-button :disabled="groupSaving" @click="membershipVisible = false">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="saveMembership">
          {{ membershipDraft ? '应用到编辑' : '保存分组' }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="userInfoDialog"
      :title="title"
      width="min(640px, 94vw)"
      class="premium-dialog"
      :close-on-click-modal="!submitting"
      :close-on-press-escape="!submitting"
      :show-close="!submitting"
    >
      <div class="dialog-content">
        <el-form label-position="top" class="account-form">
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

          <el-form-item label="所属分组" class="account-form-wide">
            <div class="membership-summary">
              <div class="membership-tags">
                <el-tag
                  v-for="group in selectedMembershipGroups"
                  :key="group.id"
                  class="membership-tag"
                  :class="{ 'membership-tag-default': group.id === 0 }"
                  effect="light"
                >
                  {{ group.name }}
                </el-tag>
              </div>
              <el-button
                v-if="canManageMembership"
                class="membership-manage-button"
                type="primary"
                plain
                :disabled="submitting || groupSaving || groupsLoading || Boolean(groupsError)"
                @click="openMembershipDialog"
              >
                分组管理
              </el-button>
            </div>
          </el-form-item>

          <div class="security-edit-grid account-form-wide">
            <el-tag v-if="editModel === 'create'" class="account-form-wide" type="info">
              创建后配置安全与发信
            </el-tag>
            <div class="security-edit-item">
              <div>
                <strong>安全码</strong>
                <span v-if="recoveryCodeWebManaged">
                  {{ recoveryCodeMeta(selectedUser?.RecoveryCodeConfigured).label }}
                </span>
                <span v-else-if="editModel === 'edit'">部署端管理</span>
              </div>
              <el-button
                :disabled="!recoveryCodeWebManaged || submitting"
                :loading="securityDialogOpening === 'recovery-code'"
                @click="openRecoveryCodeDialog"
              >
                {{ recoveryCodeWebManaged || editModel === 'create' ? '管理安全码' : '仅支持本机管理' }}
              </el-button>
            </div>
            <div class="security-edit-item">
              <div>
                <strong>外部邮箱</strong>
                <span>
                  {{ externalEmailMeta(
                    selectedUser?.ExternalEmailConfigured
                  ).label }}
                </span>
              </div>
              <el-button
                :disabled="!selectedUser?.ID || submitting"
                :loading="securityDialogOpening === 'external-email'"
                @click="openExternalEmailDialog"
              >
                编辑外部邮箱
              </el-button>
            </div>
            <div v-if="canManageNotification" class="security-edit-item account-form-wide">
              <strong>系统通知发信</strong>
              <el-button
                :disabled="!selectedUser?.ID || submitting"
                @click="notificationDialogVisible = true"
              >管理发信授权</el-button>
            </div>
          </div>

          <el-form-item class="account-form-wide">
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
            {{ editModel === 'create' ? '创建并继续配置' : lang.confirm }}
          </el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog
      v-model="notificationDialogVisible"
      title="系统通知发信"
      class="notification-dialog"
      width="min(980px, 94vw)"
      top="5vh"
      append-to-body
      destroy-on-close
      :before-close="closeNotificationDialog"
    >
      <SystemNotification
        v-if="notificationDialogVisible && selectedUser?.ID"
        :key="selectedUser.ID"
        ref="notificationPanel"
        class="notification-compact"
        compact
        :user-id="selectedUser.ID"
      />
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
import SystemNotification from "../SystemNotification/SystemNotification.vue";

const {
  lang,
  accountGroups,
  groupsLoading,
  groupsError,
  canManageGroups,
  canManageMembership,
  groupSaving,
  membershipVisible,
  membershipUser,
  membershipSelection,
  membershipLimit,
  membershipDraft,
  membershipSingleSelection,
  createMembershipGroup,
  loadGroup,
  toggleGroup,
  saveGroup,
  editMembership,
  saveMembership,
  createUser,
  handleEdit,
  canEditUser,
  reflushList,
  userInfoDialog,
  title,
  submit,
  editModel,
  editUserInfo,
  availableRoles,
  canCreateUser,
  canEditSelectedUser,
  canSelectRole,
  roleChanged,
  submitting,
  selectedUser,
  selectedMembershipGroups,
  openMembershipDialog,
  editingCurrentUser,
  recoveryCodeWebManaged,
  recoveryCodeDialogVisible,
  externalEmailDialogVisible,
  notificationDialogVisible,
  notificationPanel,
  canManageNotification,
  closeNotificationDialog,
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
