import axios from 'axios'
import type { AxiosResponse, AxiosInstance } from 'axios'
import { pluginSettingsResource } from '@/api/resource'

// 创建 Axios 实例
const service: AxiosInstance = axios.create({
  baseURL: pluginSettingsResource, // 插件配置 API 基础路径
  timeout: 5000, // 请求超时时间
  headers: {
    'Content-Type': 'application/json;charset=utf-8', // 请求体内容类型
  },
})

// 添加请求拦截器
service.interceptors.request.use(
  (config) => {
    return config
  },
  (error) => {
    return Promise.reject(error)
  },
)

// 添加响应拦截器
service.interceptors.response.use(
  (response: AxiosResponse) => {
    const res = response.data
    // errorNo 为 0 表示成功，其他值由主程序统一返回中文错误。
    if (res.errorNo === 0) {
      return res
    }
    return Promise.reject(new Error(res.errorMsg || '请求失败'))
  },
  (error) => {
    const message = error?.response?.data?.errorMsg || error?.message || '请求失败'
    return Promise.reject(new Error(message))
  },
)

export default service
