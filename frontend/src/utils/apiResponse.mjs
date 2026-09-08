const maximumErrorBytes = 1024 * 1024

// 下载接口的 JSON 业务错误也会被 Axios 包装成 Blob。
// 仅解开小体积错误信封，真实 CSV、JSON 导出和未知内容保持原样。
export async function decodeBlobAPIError(response) {
  const payload = response?.data
  if (!(payload instanceof Blob) || payload.size > maximumErrorBytes) return
  const contentType = payload.type || response.headers?.['content-type'] || ''
  if (!/(?:application|text)\/(?:[\w.-]+\+)?json\b/i.test(contentType)) return
  try {
    const body = JSON.parse(await payload.text())
    if (body && !Array.isArray(body) && typeof body.errorNo === 'number' && body.errorNo !== 0) {
      response.data = body
    }
  } catch {
    // 非 JSON 内容仍交给原有下载校验处理，不能掩盖原始 HTTP 错误。
  }
}
