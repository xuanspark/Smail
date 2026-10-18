<template>
<div class="mail-detail-container">
    <div class="mail-detail-header">
      <el-button @click="backToList" plain class="back-btn" aria-label="返回邮件列表">
        <el-icon><Back /></el-icon>
      </el-button>
      <div v-if="!loading && !loadError" class="action-buttons">
        <el-button
          :type="isTranslated ? 'warning' : 'default'"
          :loading="translating"
          plain
          @click="toggleTranslate"
          class="translate-btn"
        >
          <el-icon><Operation /></el-icon>
          <span>{{ isTranslated ? '查看原文' : '一键翻译' }}</span>
        </el-button>
        <el-button
          v-if="detailData.type === 0"
          type="primary"
          @click="handleReply"
        >
          {{ lang.mail_reply }}
        </el-button>
        <el-button plain @click="handleDelete" aria-label="删除邮件">
          <el-icon><Delete /></el-icon>
        </el-button>
      </div>
    </div>

    <div v-if="loading" class="mail-detail-state" role="status" aria-live="polite">
      <p>正在读取邮件…</p>
    </div>
    <div v-else-if="loadError" class="mail-detail-state" role="alert">
      <h1>邮件暂时无法查看</h1>
      <p>{{ loadError }}</p>
      <div class="mail-detail-state-actions">
        <el-button type="primary" @click="loadDetail">重试</el-button>
        <el-button @click="backToList">返回邮件列表</el-button>
      </div>
    </div>
    <div v-else class="mail-detail-content">
      <el-alert
        v-if="detailData.is_blacklisted"
        title="该发件人已在你的黑名单中"
        description="这是已保存的历史邮件。黑名单会拦截后续新邮件，不会自动删除历史邮件。请谨慎查看内容与附件。"
        type="error"
        :closable="false"
        show-icon
      />

      <div v-if="isTranslated" class="translation-banner">
        <el-icon class="banner-icon"><Operation /></el-icon>
        <span>已自动翻译为中文（源语言：{{ detectedLang || '自动识别' }}）</span>
        <el-button link type="primary" @click="toggleTranslate" class="toggle-back-link">查看原文</el-button>
      </div>

      <h1 class="mail-subject">{{ displaySubject }}</h1>
      
      <div class="mail-meta-card">
        <div class="meta-left">
          <div class="avatar-placeholder">
            {{ getInitial(formatSenderName(detailData.from_name, detailData.from_address)) }}
          </div>
          <div class="meta-info">
            <div class="sender-line">
              <span class="sender-name">{{ formatSenderName(detailData.from_name, detailData.from_address) }}</span>
              <span class="sender-email" v-if="hasSenderName(detailData.from_name)">&lt;{{ detailData.from_address }}&gt;</span>
            </div>
            <div class="receivers-line">
              <span class="meta-label">{{ lang.to }}:</span>
              <span v-for="(to, index) in tos" :key="index" class="receiver-chip">
                {{ to.Name !== '' ? to.Name : to.EmailAddress }}<span v-if="index < tos.length - 1">, </span>
              </span>
              <span v-if="showCC" class="cc-section">
                <span class="meta-label">{{ lang.cc }}:</span>
                <span v-for="(item, index) in ccs" :key="'cc'+index" class="receiver-chip">
                  {{ item.Name !== '' ? item.Name : item.EmailAddress }}<span v-if="index < ccs.length - 1">, </span>
                </span>
              </span>
              <span v-if="showBCC" class="cc-section">
                <span class="meta-label">{{ lang.bcc }}:</span>
                <span v-for="(item, index) in bccs" :key="'bcc'+index" class="receiver-chip">
                  {{ item.Name || item.EmailAddress }}<span v-if="index < bccs.length - 1">, </span>
                </span>
              </span>
            </div>
          </div>
        </div>
        <div class="meta-right">
          <div class="mail-date">{{ formatDetailDate(detailData.send_date) }}</div>
        </div>
      </div>

      <el-divider class="custom-divider"/>

      <div class="mail-body">
        <div class="body-text" v-if="displayIsText">
          {{ displayContent }}
        </div>
        <div class="body-html" v-else v-html="displayContent"></div>
      </div>

      <div v-if="detailData.attachments && detailData.attachments.length > 0" class="attachments-section">
        <el-divider class="custom-divider"/>
        <div class="attachments-title">{{ lang.attachment }} ({{ detailData.attachments.length }})</div>
        <div class="attachments-list">
          <a class="attachment-card" v-for="item in detailData.attachments" :key="item.Index" :href="'/attachments/download/' + detailData.id + '/' + item.Index">
            <div class="att-icon"><el-icon><Document/></el-icon></div>
            <div class="att-name">{{ item.Filename }}</div>
            <div class="att-download"><el-icon><Download/></el-icon></div>
          </a>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { usePage, Back, Delete, Document, Download, Operation } from './EmailDetail.js'

const {
  handleReply,
  handleDelete,
  loading,
  loadError,
  loadDetail,
  backToList,
  detailData,
  getInitial,
  lang,
  tos,
  showCC,
  ccs,
  showBCC,
  bccs,
  formatSenderName,
  hasSenderName,
  formatDetailDate,
  isTranslated,
  translating,
  detectedLang,
  displaySubject,
  displayContent,
  displayIsText,
  toggleTranslate
} = usePage()
</script>

<style scoped src="./EmailDetail.css"></style>
