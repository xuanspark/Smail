package config

import "sync"

var runtimeManager struct {
	sync.RWMutex
	value *ConfigManager
}

// SetRuntimeManager 注入当前进程使用的配置管理器。
func SetRuntimeManager(manager *ConfigManager) {
	runtimeManager.Lock()
	previous := runtimeManager.value
	runtimeManager.value = manager
	runtimeManager.Unlock()
	if previous != nil && previous != manager {
		previous.releasePendingSSL()
	}
}

// CurrentManager 返回当前配置管理器；未完成分区配置初始化时返回 nil。
func CurrentManager() *ConfigManager {
	runtimeManager.RLock()
	defer runtimeManager.RUnlock()
	return runtimeManager.value
}
