package config

import (
	"io"

	log "github.com/sirupsen/logrus"
)

// 无返回值的租约释放接口记录关闭失败，不将路径或驱动错误中的敏感信息写入日志。
func releaseConfigLease(unlock func(), lease io.Closer) {
	unlock()
	closeConfigResource(lease)
}

func closeConfigResource(resource io.Closer) {
	if err := resource.Close(); err != nil {
		log.WithField("action", "释放配置资源").Error("关闭配置资源失败，请检查文件句柄和数据库连接状态")
	}
}
