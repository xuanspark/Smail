<template>
<div class="mail-detail-container">
    <div class="mail-detail-header">
      <el-button @click="$router.back()" plain class="back-btn">
        <el-icon><Back /></el-icon>
      </el-button>
      <div class="action-buttons">
        <el-button plain @click="handleDelete">
          <el-icon><Delete /></el-icon>
        </el-button>
      </div>
    </div>

    <div class="mail-detail-content">
      <h1 class="mail-subject">{{ detailData.subject }}</h1>
      
      <div class="mail-meta-card">
        <div class="meta-left">
          <div class="avatar-placeholder">
            {{ getInitial(detailData.from_name || detailData.from_address) }}
          </div>
          <div class="meta-info">
            <div class="sender-line">
              <span class="sender-name">{{ detailData.from_name !== '' ? detailData.from_name : detailData.from_address }}</span>
              <span class="sender-email" v-if="detailData.from_name !== ''">&lt;{{ detailData.from_address }}&gt;</span>
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
            </div>
          </div>
        </div>
        <div class="meta-right">
          <div class="mail-date">{{ formatDetailDate(detailData.send_date) }}</div>
        </div>
      </div>

      <el-divider class="custom-divider"/>

      <div class="mail-body">
        <div class="body-text" v-if="detailData.html === ''">
          {{ detailData.text }}
        </div>
        <div class="body-html" v-else v-html="detailData.html"></div>
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
import { usePage, Back, Delete, Document, Download } from './EmailDetail.js'

const {
  handleDelete,
  detailData,
  getInitial,
  lang,
  tos,
  showCC,
  ccs,
  formatDetailDate
} = usePage()
</script>

<style scoped src="./EmailDetail.css"></style>
