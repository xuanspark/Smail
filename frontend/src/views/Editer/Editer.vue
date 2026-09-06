<template>
<div class="composer-container">
    <div class="composer-header">
      <h2 class="composer-title">{{ lang.compose }}</h2>
      <div class="composer-actions">
        <el-button class="action-btn" @click="upload">
          <el-icon><Paperclip /></el-icon> {{ lang.add_att }}
        </el-button>
        <el-button type="primary" class="send-btn" @click="send(ruleFormRef)">
          <el-icon><Position /></el-icon> {{ lang.send }}
        </el-button>
        <input v-show="false" ref="fileRef" type="file" @change="fileChange" multiple>
      </div>
    </div>

    <div class="composer-body">
      <el-form :rules="rules" ref="ruleFormRef" :model="ruleForm" class="composer-form">
        
        <div class="compose-row">
          <div class="field-label">{{ lang.sender }}</div>
          <el-form-item prop="sender" class="flex-grow-item m-0">
            <el-popover trigger="click" :width="400" placement="bottom-start">
              <template #reference>
                <div class="sender-selector">
                  <span class="sender-name">{{ ruleForm.nickName }}</span>
                  <span class="sender-email">&lt;{{ ruleForm.sender }}@{{ ruleForm.pick域名 }}&gt;</span>
                  <el-icon class="arrow-down"><ArrowDown /></el-icon>
                </div>
              </template>
              <template #default>
                <div class="sender-edit-card">
                  <div class="edit-row">
                    <span class="edit-label">前缀</span>
                    <el-input 
                      :disabled="!(globalStatus.userInfos.is_admin)"
                      v-model="ruleForm.sender" 
                      :placeholder="lang.sender_desc"
                    />
                  </div>
                  <div class="edit-row">
                    <span class="edit-label">域名</span>
                    <el-select v-model="ruleForm.pick域名" class="w-full">
                      <el-option :value="item" v-for="item in ruleForm.domains" :key="item">{{ item }}</el-option>
                    </el-select>
                  </div>
                  <div class="edit-row">
                    <span class="edit-label">{{ lang.nick_name }}</span>
                    <el-input v-model="ruleForm.nickName"/>
                  </div>
                </div>
              </template>
            </el-popover>
          </el-form-item>
        </div>

        <div class="compose-row">
          <div class="field-label">{{ lang.to }}</div>
          <el-form-item prop="receivers" class="flex-grow-item m-0 border-less-select">
            <el-select 
              v-model="ruleForm.receivers" 
              multiple filterable allow-create :reserve-keyword="false" 
              placeholder="请输入收件人地址"
            ></el-select>
          </el-form-item>
        </div>

        <div class="compose-row" v-if="ruleForm.cc.length > 0 || showCcBcc">
          <div class="field-label">{{ lang.cc }}</div>
          <el-form-item prop="cc" class="flex-grow-item m-0 border-less-select">
            <el-select 
              v-model="ruleForm.cc" 
              multiple filterable allow-create :reserve-keyword="false" 
              placeholder="请输入抄送人地址"
            ></el-select>
          </el-form-item>
        </div>

        <div class="compose-row" v-if="ruleForm.bcc.length > 0 || showCcBcc">
          <div class="field-label">{{ lang.bcc }}</div>
          <el-form-item prop="bcc" class="flex-grow-item m-0 border-less-select">
            <el-select 
              v-model="ruleForm.bcc" 
              multiple filterable allow-create :reserve-keyword="false" 
              placeholder="请输入密送人地址"
            ></el-select>
          </el-form-item>
        </div>

        <div class="compose-row subject-row">
          <el-form-item prop="subject" class="w-full m-0 border-less-input">
            <el-input v-model="ruleForm.subject" placeholder="请输入邮件主题"></el-input>
            <div class="cc-bcc-toggle" @click="showCcBcc = !showCcBcc" v-if="!showCcBcc">抄送/密送</div>
          </el-form-item>
        </div>

        <div class="editor-wrapper">
          <Toolbar class="editor-toolbar" :editor="editorRef" :defaultConfig="toolbarConfig" :mode="mode"/>
          <Editor class="editor-content" v-model="valueHtml" :defaultConfig="editorConfig" :mode="mode" @onCreated="handleCreated"/>
        </div>

        <div class="attachments-preview" v-if="fileList.length > 0">
          <div class="att-chip" v-for="(item, index) in fileList" :key="index">
            <el-icon class="att-icon"><Document /></el-icon>
            <span class="att-name">{{ item.name }}</span>
            <el-icon class="att-remove" @click="delFile(index)"><Close/></el-icon>
          </div>
        </div>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { usePage, Paperclip, Position, ArrowDown, Toolbar, Editor, Document, Close } from './Editer.js'

const {
  lang,
  upload,
  send,
  ruleFormRef,
  fileRef,
  fileChange,
  rules,
  ruleForm,
  globalStatus,
  showCcBcc,
  editorRef,
  toolbarConfig,
  mode,
  valueHtml,
  editorConfig,
  handleCreated,
  fileList,
  delFile
} = usePage()
</script>

<style scoped src="./Editer.css"></style>
