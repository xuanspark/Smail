<template>
  <div class="third-party-authorization" v-loading="protocolLoading">
    <section class="authorization-section">
      <div class="authorization-header">
        <div class="header-main">
          <h3>???????</h3>
          <p class="authorization-desc">
            ?? IMAP?POP3?SMTP ????????????????????????????
          </p>
        </div>
        <div class="header-actions">
          <el-button
            v-if="hasConfiguredProtocols && !codesRevealed"
            type="primary"
            plain
            size="small"
            @click="openVerifyDialog"
          >
            ?????????
          </el-button>
          <el-button
            v-else-if="codesRevealed"
            type="info"
            plain
            size="small"
            @click="hideRevealedCodes"
          >
            ???????
          </el-button>
        </div>
      </div>

      <el-alert
        title="???????????????????????????????????????????????????????????????"
        type="info"
        :closable="false"
        show-icon
      />

      <div class="protocol-list">
        <div
          v-for="item in protocolCredentials"
          :key="item.protocol"
          class="protocol-item"
        >
          <div class="protocol-info">
            <div class="protocol-title">
              <strong>{{ protocolLabel(item.protocol) }}</strong>
              <el-tag size="small" :type="item.enabled ? 'success' : 'info'">
                {{ item.enabled ? '???' : '???' }}
              </el-tag>
              <el-tag size="small" :type="item.configured ? 'success' : 'warning'">
                {{ item.configured ? '??????' : '??????' }}
              </el-tag>
            </div>
            <p class="protocol-guide">???????????????????????</p>

            <div class="protocol-credential-slot">
              <span class="slot-label">??????</span>
              <template v-if="item.configured">
                <template v-if="codesRevealed && revealedCodes[item.protocol]">
                  <code class="revealed-code">{{ revealedCodes[item.protocol] }}</code>
                  <el-button
                    type="primary"
                    link
                    size="small"
                    class="copy-btn"
                    @click="copyProtocolCode(revealedCodes[item.protocol])"
                  >
                    ??
                  </el-button>
                </template>
                <template v-else>
                  <span class="masked-code">????????????????</span>
                  <el-button
                    type="primary"
                    link
                    size="small"
                    class="reveal-btn"
                    @click="openVerifyDialog"
                  >
                    ??????
                  </el-button>
                </template>
              </template>
              <template v-else>
                <span class="unconfigured-tip">???????????????????</span>
              </template>
            </div>
          </div>

          <div class="protocol-actions">
            <el-switch
              :model-value="item.enabled"
              :loading="protocolLoading"
              active-text="??"
              inactive-text="??"
              :aria-label="`${protocolLabel(item.protocol)} ?????????`"
              @change="value => setProtocolEnabled(item, value)"
            />
            <el-button
              type="primary"
              size="small"
              :disabled="!item.enabled"
              @click="generateProtocolCode(item)"
            >
              {{ item.configured ? '???????' : '?????' }}
            </el-button>
            <el-button
              v-if="item.configured"
              type="danger"
              plain
              size="small"
              @click="revokeProtocolCode(item)"
            >
              ?????
            </el-button>
          </div>
        </div>
      </div>
    </section>

    <!-- ???????? -->
    <el-dialog
      v-model="verifyDialogVisible"
      title="??????"
      width="min(440px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      class="verify-reveal-dialog"
      @close="closeVerifyDialog"
    >
      <div class="verify-dialog-body">
        <p class="verify-dialog-tip">
          ?????????????????????????????????
        </p>
        <el-form @submit.prevent="submitVerifyAndReveal">
          <el-form-item label="??????">
            <el-input
              v-model="verifyPassword"
              type="password"
              show-password
              placeholder="???????????"
              autocomplete="current-password"
              @keyup.enter="submitVerifyAndReveal"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="verify-dialog-footer">
          <el-button @click="closeVerifyDialog">??</el-button>
          <el-button
            type="primary"
            :loading="verifyLoading"
            :disabled="!verifyPassword.trim()"
            @click="submitVerifyAndReveal"
          >
            ???????
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { usePage } from './ThirdPartyAuthorization.js'

const {
  protocolLoading,
  protocolCredentials,
  revealedCodes,
  codesRevealed,
  verifyDialogVisible,
  verifyPassword,
  verifyLoading,
  protocolLabel,
  setProtocolEnabled,
  generateProtocolCode,
  copyProtocolCode,
  revokeProtocolCode,
  openVerifyDialog,
  closeVerifyDialog,
  submitVerifyAndReveal,
  hideRevealedCodes,
} = usePage()

const hasConfiguredProtocols = computed(() =>
  Array.isArray(protocolCredentials.value) &&
  protocolCredentials.value.some(item => item.configured)
)
</script>

<style scoped src="./ThirdPartyAuthorization.css"></style>
