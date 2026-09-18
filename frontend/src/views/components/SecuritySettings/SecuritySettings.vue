<template>
  <div class="settings-card security-settings-card" v-loading="securityLoading">
    <div class="security-workspace">
      <SettingsSubmenu
        title="安全设置"
        :items="sectionOptions"
        :model-value="selectedSection"
        @select="selectSection"
      />

      <div class="security-main">
    <ProfileSettings v-if="selectedSection === 'profile'" />
    <GlobalSecurityPolicy
      v-if="selectedSection === 'security-policy'"
      ref="globalSecurityPolicyPage"
    />

    <section v-if="selectedSection === 'password'" class="security-section">
      <div class="settings-header">
        <h3>{{ lang.modify_pwd }}</h3>
        <p class="settings-desc">{{ isSuperAdmin ? '通过已验证安全邮箱修改密码，优先主要邮箱；完成后需重新登录。' : lang.modify_pwd_desc }}</p>
      </div>
      <div class="settings-body">
        <el-form :model="ruleForm" :rules="rules" status-icon label-position="top" class="settings-form">
          <template v-if="isSuperAdmin">
            <el-form-item label="外部安全邮箱">
              <div v-if="passwordEmail" class="password-verification-row">
                <strong>{{ passwordEmail.email }}</strong>
                <el-button
                  :loading="passwordSending"
                  :disabled="passwordCountdown > 0 || passwordSubmitting"
                  @click="sendPasswordCode"
                >
                  {{ passwordCountdown > 0 ? `${passwordCountdown} 秒后重发` : '获取验证码' }}
                </el-button>
              </div>
              <el-button v-else type="primary" link @click="selectSection('external-email')">
                请先绑定并验证外部安全邮箱
              </el-button>
            </el-form-item>
            <el-form-item label="邮箱验证码" required>
              <el-input
                v-model="ruleForm.code"
                inputmode="numeric"
                autocomplete="one-time-code"
                maxlength="8"
                placeholder="请输入邮箱中的 8 位验证码"
                :disabled="!passwordEmail || passwordSubmitting"
              />
            </el-form-item>
          </template>
          <el-form-item v-else :label="lang.current_pwd" prop="current_pwd">
            <el-input v-model="ruleForm.current_pwd" type="password" autocomplete="current-password" :placeholder="lang.enter_current_pwd" show-password class="premium-input" />
          </el-form-item>
          <el-form-item :label="lang.modify_pwd" prop="new_pwd">
            <el-input v-model="ruleForm.new_pwd" type="password" autocomplete="new-password" :placeholder="lang.enter_new_pwd" show-password class="premium-input" />
          </el-form-item>
          <el-form-item :label="lang.enter_again" prop="new_pwd2">
            <el-input v-model="ruleForm.new_pwd2" type="password" autocomplete="new-password" :placeholder="lang.confirm_new_pwd" show-password class="premium-input" />
          </el-form-item>
          <div class="form-actions">
            <el-button type="primary" :loading="passwordSubmitting" :disabled="passwordSending || (isSuperAdmin && !passwordEmail)" @click="submit" class="submit-btn">
              {{ lang.submit }}
            </el-button>
          </div>
        </el-form>
      </div>
      <el-divider class="settings-divider" />
    </section>

    <section v-if="selectedSection === 'question'" class="security-section">
      <div class="settings-header">
        <h3>密保问题</h3>
        <p v-if="isSuperAdmin" class="settings-desc">密保不用于超级管理员重置密码，请使用修改密码页面的邮箱验证或部署端本机恢复通道。</p>
        <p v-else class="settings-desc">密保问题不能单独重置密码，必须与离线恢复码组合使用。</p>
      </div>
      <el-form :model="questionForm" label-position="top" class="settings-form">
        <el-form-item label="密保问题"><el-input v-model="questionForm.question" placeholder="例如：我最喜欢的颜色是什么？" /></el-form-item>
        <el-form-item label="密保答案"><el-input v-model="questionForm.answer" type="password" show-password placeholder="请输入答案，保存后不会回显" /></el-form-item>
        <div class="form-actions"><el-button type="primary" @click="saveSecurityQuestion">{{ securityQuestion.configured ? '更新密保问题' : '设置密保问题' }}</el-button></div>
      </el-form>
    </section>

    <el-divider v-if="selectedSection === 'recovery'" class="settings-divider" />
    <section v-if="selectedSection === 'recovery' && !isSuperAdmin" class="security-section">
      <div class="settings-header security-key-header">
        <div><h3>离线恢复码</h3><p class="settings-desc">恢复码只在生成时显示一次，请保存到安全位置。</p></div>
        <div class="section-actions"><el-button type="primary" @click="generateRecoveryCodes">生成新恢复码</el-button><el-button v-if="recoveryConfigured" type="danger" plain @click="revokeRecoveryCodes">撤销</el-button></div>
      </div>
      <el-alert v-if="recoveryConfigured" title="当前已配置有效离线恢复码" type="success" :closable="false" show-icon />
      <el-alert v-else title="当前未配置有效离线恢复码，高风险操作可能无法进行。" type="warning" :closable="false" show-icon />
      <div v-if="generatedCodes.length" class="recovery-code-panel">
        <div class="one-time-tip">以下内容只显示本次，请立即复制并妥善保存；离开页面后不会再次读取。</div>
        <code v-for="code in generatedCodes" :key="code" class="recovery-code">{{ code }}</code>
      </div>
    </section>

    <el-divider v-if="selectedSection === 'external-email'" class="settings-divider" />
    <section v-if="selectedSection === 'external-email'" class="security-section">
      <div class="settings-header external-email-header">
        <div><h3>外部安全邮箱</h3><p class="settings-desc">用于登录二次验证和密码恢复，只显示脱敏地址。</p></div>
        <el-button type="primary" @click="openExternalEmailDialog">绑定外部邮箱</el-button>
      </div>
      <div v-if="externalEmails.length" v-loading="externalListLoading" class="external-list">
        <div v-for="item in externalEmails" :key="item.id" class="external-item">
          <div><strong>{{ item.email }}</strong><span>{{ item.type === 'primary' ? '主要邮箱' : '备用邮箱' }}</span><el-tag size="small" :type="item.status === 'verified' ? 'success' : 'warning'">{{ item.status === 'verified' ? '已验证' : item.status === 'pending' ? '待验证' : '已停用' }}</el-tag></div>
          <el-button v-if="item.status !== 'disabled'" type="danger" plain size="small" @click="disableExternalEmail(item)">停用</el-button>
        </div>
      </div>
      <el-empty v-else v-loading="externalListLoading" description="暂无外部安全邮箱" />
    </section>

    <TOTPSettings v-if="selectedSection === 'totp'" />

    <el-divider v-if="selectedSection === 'webauthn'" class="settings-divider" />
    <el-alert
      v-if="selectedSection === 'webauthn' && !webAuthnEnabled"
      title="服务器尚未启用安全密钥，请联系部署管理员配置 WebAuthn 后重试。"
      type="info"
      :closable="false"
      show-icon
    />
    <section v-if="selectedSection === 'webauthn' && supported && webAuthnEnabled" class="security-section">
      <div class="settings-header security-key-header"><div><h3>WebAuthn 安全密钥</h3><p class="settings-desc">使用设备或浏览器内置的安全密钥无密码登录。</p></div><el-button type="primary" :loading="registering" @click="registerCredential">注册安全密钥</el-button></div>
      <div class="credential-list" v-loading="credentialsLoading"><el-empty v-if="!credentialsLoading && credentials.length === 0" description="暂无已注册的安全密钥" /><div v-for="item in credentials" :key="item.id" class="credential-item"><div class="credential-info"><strong>{{ item.display_name }}</strong><span>创建于{{ formatTime(item.created_at) }}</span><span v-if="item.last_used_at">最后使用于{{ formatTime(item.last_used_at) }}</span><el-tag v-if="item.revoked_at" type="info" size="small">已撤销</el-tag><el-tag v-else-if="item.requires_re_registration" type="warning" size="small">需要重新注册</el-tag><el-tag v-else type="success" size="small">有效</el-tag></div><div class="credential-actions"><el-button size="small" :disabled="Boolean(item.revoked_at)" @click="renameCredential(item)">重命名</el-button><el-button size="small" type="danger" plain :disabled="Boolean(item.revoked_at)" @click="revokeCredential(item)">撤销</el-button></div></div></div>
    </section>
    <el-alert v-if="selectedSection === 'webauthn' && !supported" title="当前浏览器不支持 WebAuthn，请使用 HTTPS 和受支持的浏览器。" type="warning" :closable="false" show-icon />


    <ExternalEmailBindingDialog
      v-model="externalDialogVisible"
      @bound="handleExternalEmailBound"
    />
      </div>
    </div>
  </div>
</template>

<script setup>
import { usePage } from './SecuritySettings.js'
import ProfileSettings from './ProfileSettings/ProfileSettings.vue'
import TOTPSettings from './TOTPSettings/TOTPSettings.vue'
import SettingsSubmenu from '../SettingsSubmenu/SettingsSubmenu.vue'
import GlobalSecurityPolicy from './GlobalSecurityPolicy/GlobalSecurityPolicy.vue'
import ExternalEmailBindingDialog from './ExternalEmailBindingDialog/ExternalEmailBindingDialog.vue'

const {
  lang, isSuperAdmin, supported, webAuthnEnabled, sectionOptions, selectedSection, selectSection,
  globalSecurityPolicyPage,
  passwordEmail, passwordSending, passwordSubmitting, passwordCountdown, sendPasswordCode,
  registering, credentialsLoading, credentials, securityLoading,
  securityQuestion, questionForm, recoveryConfigured, generatedCodes, externalEmails, externalListLoading,
  externalDialogVisible, ruleForm, rules, submit, registerCredential, renameCredential, revokeCredential,
  saveSecurityQuestion, generateRecoveryCodes, revokeRecoveryCodes, openExternalEmailDialog,
  handleExternalEmailBound,
  disableExternalEmail, formatTime,
} = usePage()
</script>

<style scoped src="./SecuritySettings.css"></style>
