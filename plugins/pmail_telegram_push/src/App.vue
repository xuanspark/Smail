<template>
  <div class="pmail-telegram-push-settings">
    <el-card class="box-card">
      <template #header>
        <div class="card-header">
          <div>
            <div class="card-title">Telegram 推送设置</div>
            <div class="card-description">为当前账户配置独立的 Telegram 邮件通知。</div>
          </div>
          <el-badge :value="saved ? 1 : ''" class="item">
            <el-button
              id="contact_bot_button"
              type="primary"
              :disabled="!botInfo.botLink"
              @click="contactBot"
            >
              <TelegramIcon class="icon" />
              联系机器人
            </el-button>
          </el-badge>
        </div>
      </template>

      <el-form
        v-loading="loading"
        :model="formData"
        :rules="rules"
        label-width="180px"
        label-position="left"
      >
        <el-form-item label="Telegram 聊天 ID" prop="chatId">
          <el-input
            id="chat_id"
            v-model="formData.chatId"
            placeholder="请输入 Telegram 聊天 ID，留空则禁用推送"
          />
        </el-form-item>

        <el-form-item label="显示邮件内容">
          <el-switch
            v-model="formData.showContent"
            :disabled="formData.chatId.trim().length === 0"
          />
        </el-form-item>

        <el-form-item label="邮件内容防剧透">
          <el-switch
            v-model="formData.spoilerContent"
            :disabled="formData.chatId.trim().length === 0"
          />
        </el-form-item>

        <el-form-item label="发送附件">
          <el-switch
            v-model="formData.sendAttachments"
            :disabled="formData.chatId.trim().length === 0"
          />
        </el-form-item>

        <el-form-item label="禁用链接预览">
          <el-switch
            v-model="formData.disableLinkPreview"
            :disabled="formData.chatId.trim().length === 0"
          />
        </el-form-item>
      </el-form>

      <el-form-item>
        <div class="form-item-buttons">
          <el-button
            id="test_message_button"
            type="success"
            :disabled="loading || formData.chatId.trim().length === 0"
            @click="postTestMessage"
          >
            <LinkIcon class="icon" />
            测试消息
          </el-button>
          <el-button id="save_button" type="primary" :disabled="loading" @click="confirmSubmit">
            <SaveIcon class="icon" />
            保存设置
          </el-button>
        </div>
      </el-form-item>

      <el-tour v-model="tourOpen" @finish="finishTour" @close="finishTour">
        <el-tour-step
          target="#chat_id"
          title="第一步"
          description="请输入聊天 ID，留空则禁用推送"
        />
        <el-tour-step
          target="#contact_bot_button"
          title="第二步"
          description="点击联系机器人按钮，与机器人进行一次交互。"
        />
        <el-tour-step
          target="#test_message_button"
          title="第三步"
          description="点击测试消息按钮，测试是否能成功发送消息。"
        />
        <el-tour-step
          target="#save_button"
          title="第四步"
          description="点击保存设置按钮，保存当前配置。"
        />
        <template #indicators="{ current, total }">
          <span>{{ current + 1 }} / {{ total }}</span>
        </template>
      </el-tour>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import SaveIcon from '@/assets/icons/save-line.svg'
import TelegramIcon from '@/assets/icons/telegram.svg'
import LinkIcon from '@/assets/icons/link.svg'
import type { BotInfo, Setting } from '@/types'
import { getBotInfo, testMessage } from '@/api/bot'
import { getSetting, updateSetting } from '@/api/setting'
import resizeIframeHeight from '@/utils/resize'

const saved = ref(false)
const loading = ref(false)
const version = ref(0)
const botInfo = ref<BotInfo>({
  username: '',
  botLink: '',
})
const formData = ref<Setting>({
  chatId: '',
  showContent: true,
  spoilerContent: true,
  sendAttachments: true,
  disableLinkPreview: true,
})
const rules = {
  chatId: [
    {
      required: false,
      message: '请输入聊天 ID，留空则禁用推送',
      trigger: 'blur',
      whitespace: false,
    },
  ],
}

// 获取机器人公开信息。
const fetchBotInfo = async () => {
  loading.value = true
  try {
    const response = await getBotInfo()
    botInfo.value = response.data
  } catch (error) {
    const message = error instanceof Error ? error.message : '获取机器人信息失败'
    ElMessage.error(message)
    console.error(error)
  } finally {
    loading.value = false
  }
}

// 联系机器人时不向新页面传递窗口控制权。
const contactBot = () => {
  if (!botInfo.value.botLink) {
    ElMessage.error('机器人链接为空')
    return
  }
  saved.value = false
  window.open(botInfo.value.botLink, '_blank', 'noopener,noreferrer')
}

// 获取当前登录用户的插件设置和版本。
const fetchSetting = async () => {
  loading.value = true
  try {
    const response = await getSetting()
    formData.value = response.data.settings
    version.value = response.data.version
  } catch (error) {
    const message = error instanceof Error ? error.message : '获取设置信息失败'
    ElMessage.error(message)
    console.error(error)
  } finally {
    loading.value = false
  }
}

// 使用乐观锁版本保存当前用户设置。
const saveSetting = async () => {
  loading.value = true
  try {
    const response = await updateSetting(formData.value, version.value)
    formData.value = response.data.settings
    version.value = response.data.version
    saved.value = true
    ElMessage.success('设置已保存')
  } catch (error) {
    const message = error instanceof Error ? error.message : '保存设置失败'
    ElMessage.error(message)
    console.error(error)
  } finally {
    loading.value = false
  }
}

// 确认保存设置。
const confirmSubmit = () => {
  ElMessageBox.confirm('确认保存设置吗？', '保存设置', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning',
  }).then(saveSetting)
}

// 测试消息只使用当前表单，不写入数据库。
const postTestMessage = async () => {
  if (!formData.value.chatId.trim()) {
    ElMessage.error('请输入聊天 ID')
    return
  }
  loading.value = true
  try {
    const response = await testMessage(formData.value)
    ElMessage.success(response.data.message)
  } catch (error) {
    const message = error instanceof Error ? error.message : '测试消息失败'
    ElMessage.error(message)
    console.error(error)
  } finally {
    loading.value = false
  }
}

const tourOpen = ref(false)
const finishTour = () => {
  localStorage.setItem('hasShownTour', 'true')
}

onMounted(() => {
  const hasShown = localStorage.getItem('hasShownTour') === 'true'
  if (!hasShown) {
    tourOpen.value = true
  }
  resizeIframeHeight()
  void fetchBotInfo()
  void fetchSetting()
})
</script>

<style scoped>
.pmail-telegram-push-settings {
  max-width: 800px;
  margin: 0 auto;
  padding: 20px;
}

.box-card {
  margin-top: 20px;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}

.card-title {
  font-size: 20px;
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.card-description {
  margin-top: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.icon {
  width: 20px;
  height: 20px;
  margin-right: 5px;
  vertical-align: middle;
  color: white;
}

.form-item-buttons {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 20px;
  width: 100%;
  margin-top: 20px;
}
</style>
