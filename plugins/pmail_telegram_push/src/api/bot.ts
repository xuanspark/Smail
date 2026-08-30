import { get, post } from '@/api'
import type { ApiResponse, BotInfo, Setting, TestMessageResult } from '@/types'
import { botInfoResource, testMessageResource } from '@/api/resource'

// 获取机器人公开信息
export function getBotInfo(): Promise<ApiResponse<BotInfo>> {
  return get<BotInfo>(botInfoResource)
}

// 使用当前表单设置发送测试消息
export function testMessage(setting: Setting): Promise<ApiResponse<TestMessageResult>> {
  return post<TestMessageResult>(testMessageResource, {
    settings: setting,
    version: 0,
  })
}
