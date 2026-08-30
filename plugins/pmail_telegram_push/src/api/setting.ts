import { get, put } from '@/api'
import type { ApiResponse, Setting, UserSettingsStatus } from '@/types'
import { userSettingsResource } from '@/api/resource'

// 获取当前用户的插件配置
export function getSetting(): Promise<ApiResponse<UserSettingsStatus>> {
  return get<UserSettingsStatus>(userSettingsResource)
}

// 更新当前用户的插件配置
export function updateSetting(
  setting: Setting,
  version: number,
): Promise<ApiResponse<UserSettingsStatus>> {
  return put<UserSettingsStatus>(userSettingsResource, {
    settings: setting,
    version,
  })
}
