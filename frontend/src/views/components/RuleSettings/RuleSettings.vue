<template>
<div class="settings-card rule-settings-card">
    <MailPolicySettings
      v-if="canReadGlobalPolicy"
      ref="mailPolicyPage"
      class="rule-mail-policy"
    />

    <section class="personal-rules" aria-labelledby="personal-rules-title">
    <div class="settings-header">
      <h3 id="personal-rules-title">{{ lang.auto_rules }}</h3>
      <p class="settings-desc">{{ lang.auto_rules_desc }}</p>
    </div>

    <div class="table-container">
      <el-table :data="pageItems.map(item => item.entry)" class="modern-table" style="width: 100%" empty-text="暂无邮件规则">
        <el-table-column prop="name" :label="lang.rule_name" min-width="150" show-overflow-tooltip/>
        <el-table-column prop="action" :label="lang.rule_do" width="120">
          <template #default="scope">
            <el-tag size="small" effect="plain" class="action-tag">{{ ActionName[scope.row.action] }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="params" :label="lang.rule_params" :formatter="formatRuleParams" min-width="150" show-overflow-tooltip/>
        <el-table-column prop="sort" :label="lang.rule_priority" width="100"/>
        <el-table-column width="100" align="right">
          <template #default="scope">
            <div class="row-actions">
              <el-button size="small" type="primary" :icon="Edit" circle text @click="editRule(scope.row)"/>
              <el-popconfirm confirm-button-text="确认" cancel-button-text="取消" :icon="InfoFilled"
                             @confirm="delRule(scope.row.id)" icon-color="#ef4444" :title="lang.del_rule_confirm">
                <template #reference>
                  <el-button size="small" type="danger" :icon="Delete" :aria-label="lang.delete" circle text/>
                </template>
              </el-popconfirm>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <ConfigPagination v-model:current-page="currentPage" :page-count="pageCount" :total="data.length" label="邮件规则" />

    <div class="form-actions">
      <el-button type="primary" @click="createRule" class="add-btn">
        <el-icon><Plus /></el-icon> {{ lang.new_rule }}
      </el-button>
    </div>
    </section>

    <el-dialog v-model="dialogVisible" :title="addRuleForm.id > 0 ? '编辑规则' : lang.new_rule" width="700px" class="premium-dialog">
      <div class="dialog-content">
        <el-form :model="addRuleForm" label-position="top">
          <div class="form-row">
            <el-form-item :label="lang.rule_name" class="flex-grow">
              <el-input v-model="addRuleForm.name" placeholder="规则名称"/>
            </el-form-item>
            <el-form-item :label="lang.rule_priority" class="w-32">
              <el-input v-model="addRuleForm.sort" type="number" oninput="value=value.replace(/[^\-\d]/g, '')"/>
            </el-form-item>
          </div>
          
          <el-divider class="custom-divider"/>
          <div class="section-title">{{ lang.rule_desc }}（匹配条件）</div>
          
          <div class="rules-list">
            <div v-for="(rule, index) in addRuleForm.rules" :key="index" class="rule-line">
              <el-select v-model="rule.field" placeholder="邮件字段" class="w-32">
                <el-option key="From" :label="lang.from" value="From"/>
                <el-option key="Subject" :label="lang.subject" value="Subject"/>
                <el-option key="To" :label="lang.to" value="To"/>
                <el-option key="Cc" :label="lang.cc" value="Cc"/>
                <el-option key="Content" :label="lang.content" value="Content"/>
              </el-select>

              <el-select v-model="rule.type" placeholder="条件类型" class="w-32">
                <el-option key="equal" :label="lang.equal" value="equal"/>
                <el-option key="contains" :label="lang.contains" value="contains"/>
                <el-option key="regex" :label="lang.regex" value="regex"/>
              </el-select>

              <el-input v-model="rule.rule" placeholder="条件值" class="flex-grow"/>
              <el-button type="danger" :icon="Delete" @click="removeRuleLine(index)" text circle/>
            </div>
          </div>
          <div class="add-condition-action">
            <el-button size="small" type="primary" plain @click="addRule()"><el-icon><Plus/></el-icon> 添加条件</el-button>
          </div>

          <el-divider class="custom-divider"/>
          <div class="section-title">{{ lang.rule_do }} ({{ lang.action }})</div>
          
          <div class="action-row">
            <el-select v-model="addRuleForm.action" :placeholder="lang.select_action" @change="ruleTypeChange" class="w-48">
              <el-option key="mark_read" :label="lang.mark_read" :value="READ"/>
              <el-option key="move" :label="lang.move" :value="MOVE"/>
              <el-option key="delete" :label="lang.delete" :value="DELETE"/>
              <el-option key="forward" :label="lang.forward" :value="FORWARD"/>
            </el-select>
            
            <el-select v-if="addRuleForm.action === 4" v-model="addRuleForm.params" @click="reflushGroupInfos" :placeholder="lang.select_folder" class="flex-grow">
              <el-option v-for="gp in groupData.list" :key="gp.id" :label="gp.name" :value="gp.id"/>
            </el-select>

            <el-input v-if="addRuleForm.action === 2" v-model="addRuleForm.params" class="flex-grow" :placeholder="lang.forward_email_address"/>
          </div>
        </el-form>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="cancelRule">{{ lang.cancel }}</el-button>
          <el-button type="primary" @click="submitRule()">
            {{ lang.submit }}
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { usePage, Plus } from './RuleSettings.js'
import { usePagination } from '../ConfigSettings/ConfigPagination/ConfigPagination.js'
import ConfigPagination from '../ConfigSettings/ConfigPagination/ConfigPagination.vue'
import MailPolicySettings from './MailPolicySettings/MailPolicySettings.vue'

const {
  lang,
  data,
  ActionName,
  Edit,
  editRule,
  createRule,
  cancelRule,
  InfoFilled,
  delRule,
  Delete,
  dialogVisible,
  addRuleForm,
  submitRule,
  removeRuleLine,
  addRule,
  ruleTypeChange,
  READ,
  MOVE,
  DELETE,
  FORWARD,
  reflushGroupInfos,
  groupData,
  formatRuleParams,
  canReadGlobalPolicy
} = usePage()
const { currentPage, pageCount, pageItems } = usePagination(data, 6)

const mailPolicyPage = ref(null)
const beforeLeave = () => mailPolicyPage.value?.beforeLeave?.() ?? true

defineExpose({ beforeLeave })
</script>

<style scoped src="./RuleSettings.css"></style>
