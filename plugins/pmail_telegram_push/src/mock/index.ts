import Mock from 'mockjs'
import type { ApiResponse, BotInfo, Setting, TestMessageResult, UserSettingsStatus } from '@/types'
import {
  botInfoResource,
  pluginSettingsResource,
  testMessageResource,
  userSettingsResource,
} from '@/api/resource'

// 模拟 Telegram 机器人信息。
const Random = Mock.Random

Mock.setup({
  timeout: '500-1000',
})

let settingVersion = 1
const defaultSetting: Setting = {
  chatId: Random.string('number', 9),
  showContent: true,
  spoilerContent: true,
  sendAttachments: true,
  disableLinkPreview: true,
}

const getStatus = (): UserSettingsStatus => ({
  pluginId: 'telegram_push',
  settings: { ...defaultSetting },
  version: settingVersion,
})

// 模拟获取 Telegram 机器人信息。
export const getBotInfoMock = Mock.mock(
  `${pluginSettingsResource}/${botInfoResource}`,
  'get',
  (): ApiResponse<BotInfo> => ({
    errorNo: 0,
    errorMsg: '',
    data: {
      username: Random.string('lower', 10),
      botLink: 'https://t.me/example_bot',
    },
  }),
)

// 模拟获取当前用户设置。
export const getSettingMock = Mock.mock(
  `${pluginSettingsResource}/${userSettingsResource}`,
  'get',
  (): ApiResponse<UserSettingsStatus> => ({
    errorNo: 0,
    errorMsg: '',
    data: getStatus(),
  }),
)

// 模拟更新当前用户设置。
export const updateSettingMock = Mock.mock(
  `${pluginSettingsResource}/${userSettingsResource}`,
  'put',
  (options: { body: string }): ApiResponse<UserSettingsStatus> => {
    const request = JSON.parse(options.body) as { settings: Setting; version: number }
    if (request.version !== settingVersion) {
      return {
        errorNo: 100,
        errorMsg: '配置已被其他操作更新，请刷新后重试',
        data: getStatus(),
      }
    }
    defaultSetting.chatId = request.settings.chatId
    defaultSetting.showContent = request.settings.showContent
    defaultSetting.spoilerContent = request.settings.spoilerContent
    defaultSetting.sendAttachments = request.settings.sendAttachments
    defaultSetting.disableLinkPreview = request.settings.disableLinkPreview
    settingVersion += 1
    return {
      errorNo: 0,
      errorMsg: '',
      data: getStatus(),
    }
  },
)

// 模拟测试消息。
export const testMessageMock = Mock.mock(
  `${pluginSettingsResource}/${testMessageResource}`,
  'post',
  (): ApiResponse<TestMessageResult> => ({
    errorNo: 0,
    errorMsg: '',
    data: {
      message: '测试消息已发送',
    },
  }),
)

export default { getBotInfoMock, getSettingMock, updateSettingMock, testMessageMock }
