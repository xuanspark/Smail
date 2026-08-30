// 定义主程序 API 响应体结构
export interface ApiResponse<T> {
  errorNo: number
  errorMsg: string
  data: T
}

// 定义 Telegram 机器人公开信息结构
export interface BotInfo {
  username: string
  botLink: string
}

// 定义当前用户的 Telegram 推送设置
export interface Setting {
  chatId: string
  showContent: boolean
  spoilerContent: boolean
  sendAttachments: boolean
  disableLinkPreview: boolean
}

// 定义带乐观锁版本的用户设置状态
export interface UserSettingsStatus {
  pluginId: string
  settings: Setting
  version: number
}

// 定义测试消息结果
export interface TestMessageResult {
  message: string
}
