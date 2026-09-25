<template>
  <div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.account_management }}</h3>
      <p class="settings-desc">{{ lang.account_management_desc }}</p>
      <p class="settings-desc">仅显示权限范围内的成员。新账号默认归入默认分组；分组及成员归属由超级管理员维护。</p>
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
        <el-table-column label="操作" align="right" :width="canManageGroups ? 126 : 68">
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
            <el-button v-if="canManageGroups" size="small" text :disabled="groupSaving" @click="editMembership(scope.row)">分组</el-button>
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
          <fieldset class="membership-options" :disabled="groupSaving">
            <legend class="membership-options-title">
              {{ membershipLimit > 1 ? '管理员可勾选多个分组' : '请选择一个分组' }}
            </legend>
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
      <p class="settings-desc">管理员可以多选；普通用户只能选择一个分组。</p>
      <p v-if="membershipDraft" class="settings-desc">
        此处仅应用到编辑草稿，点击编辑用户的“确认”后，权限级别和分组才会一起保存。
      </p>
      <el-alert title="修改归属会使该账号的旧登录会话和通知发信凭据失效，需要重新登录并配置通知凭据。" type="warning" :closable="false" />
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

          <el-form-item v-if="editModel === 'edit'" label="所属分组">
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
                v-if="canManageGroups"
                class="membership-manage-button"
                type="primary"
                plain
                :disabled="submitting || groupSaving || groupsLoading || Boolean(groupsError)"
                @click="openMembershipDialog"
              >
                分组管理
              </el-button>
            </div>
            <p v-if="canManageGroups" class="membership-hint">
              分组选择跟随当前权限级别，点击“确认”后与其他修改一起保存；取消则不保存。
            </p>
            <p v-else class="membership-hint">
              仅显示你有权查看的分组，成员归属由超级管理员维护。
            </p>
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
  accountGroups,
  groupsLoading,
  groupsError,
  canManageGroups,
  groupSaving,
  membershipVisible,
  membershipUser,
  membershipSelection,
  membershipLimit,
  membershipDraft,
  membershipSingleSelection,
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
