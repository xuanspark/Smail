package config

import "sync/atomic"

// publishedConfig 是旧配置模型的只读兼容快照。
// 新业务应通过 CurrentConfig 读取，禁止在请求处理中直接持有并修改 Instance。
var publishedConfig atomic.Pointer[Config]
var hasPublishedConfig atomic.Bool
var setupPort atomic.Int64

func IsSetupReady() bool {
	value := CurrentConfig()
	if value != nil && value.SetupState.Valid() {
		return value.SetupState == SetupStateReady
	}
	return IsInit
}

// PublishConfigSnapshot 发布一份深拷贝，供运行时只读访问。
// 调用方仍可继续使用旧的 Instance 兼容路径，但不能把返回值当作可变全局状态。
func PublishConfigSnapshot(value *Config) {
	hasPublishedConfig.Store(true)
	if value == nil {
		publishedConfig.Store(nil)
		return
	}
	clone := cloneConfig(value)
	publishedConfig.Store(clone)
}

// CurrentConfig 返回当前运行时配置的独立副本。
// 返回值可以安全地在业务代码中读取或局部修改，不会改变全局配置快照。
func CurrentConfig() *Config {
	if value := publishedConfig.Load(); value != nil {
		return cloneConfig(value)
	}
	if hasPublishedConfig.Load() {
		return nil
	}
	if Instance == nil {
		return nil
	}
	return cloneConfig(Instance)
}

// SetRuntimeSetupPort stores the temporary setup listener port outside the persisted configuration.
func SetRuntimeSetupPort(port int) {
	setupPort.Store(int64(port))
}

func RuntimeSetupPort() int {
	return int(setupPort.Load())
}

func cloneConfig(source *Config) *Config {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Domains = append([]string(nil), source.Domains...)
	clone.AccessHosts = append([]string(nil), source.AccessHosts...)
	clone.PluginAllowList = append([]string(nil), source.PluginAllowList...)
	if source.Tables != nil {
		clone.Tables = make(map[string]string, len(source.Tables))
		for key, value := range source.Tables {
			clone.Tables[key] = value
		}
	}
	if source.TablesInitData != nil {
		clone.TablesInitData = make(map[string]string, len(source.TablesInitData))
		for key, value := range source.TablesInitData {
			clone.TablesInitData[key] = value
		}
	}
	return &clone
}
