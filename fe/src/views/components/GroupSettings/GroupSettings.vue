<template>
<div class="settings-card">
    <div class="settings-header">
      <h3>{{ lang.email_folders }}</h3>
      <p class="settings-desc">{{ lang.email_folders_desc }}</p>
    </div>

    <div class="tree-container">
      <el-tree
        :expand-on-click-node="false"
        :data="data"
        :defaultExpandAll="true"
        class="custom-tree"
      >
        <template #default="{ node, data }">
          <div class="tree-node-content">
            <div class="node-label">
              <el-icon class="folder-icon"><Folder /></el-icon>
              <span v-if="data.id !== -1">{{ data.label }}</span>
              <el-input
                v-if="data.id === -1"
                v-model="data.label"
                size="small"
                :placeholder="lang.folder_name"
                class="node-input"
                @blur="onInputBlur(data)"
                @keyup.enter="onInputBlur(data)"
                ref="newInput"
                autofocus
              ></el-input>
            </div>
            
            <div class="node-actions" v-if="data.id !== 0">
              <el-button @click.stop="add(data)" size="small" type="primary" text bg class="action-btn">
                <el-icon><Plus /></el-icon>
              </el-button>
              <el-button @click.stop="del(node, data)" size="small" type="danger" text bg class="action-btn">
                <el-icon><Delete /></el-icon>
              </el-button>
            </div>
          </div>
        </template>
      </el-tree>
    </div>

    <div class="form-actions">
      <el-button type="primary" @click="addRoot" class="add-root-btn" plain>
        <el-icon><Plus /></el-icon> {{ lang.add_group }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { usePage, Folder, Plus, Delete } from './GroupSettings.js'

const {
  lang,
  data,
  onInputBlur,
  add,
  del,
  addRoot
} = usePage()
</script>

<style scoped src="./GroupSettings.css"></style>
