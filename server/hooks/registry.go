package hooks

import (
	"sort"
	"sync"

	"git.pcux.cn/xuans/Smail/hooks/framework"
)

var hookListMu sync.RWMutex

func validHookName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func ResetHooks() {
	hookListMu.Lock()
	HookList = make(map[string]framework.EmailHook)
	hookListMu.Unlock()
}

func RegisterHook(name string, hook framework.EmailHook) bool {
	if !validHookName(name) || hook == nil {
		return false
	}
	hookListMu.Lock()
	defer hookListMu.Unlock()
	if HookList == nil {
		HookList = make(map[string]framework.EmailHook)
	}
	if _, exists := HookList[name]; exists {
		return false
	}
	HookList[name] = hook
	return true
}

func GetHook(name string) (framework.EmailHook, bool) {
	hookListMu.RLock()
	hook, ok := HookList[name]
	hookListMu.RUnlock()
	return hook, ok
}

func HookNames() []string {
	hookListMu.RLock()
	names := make([]string, 0, len(HookList))
	for name := range HookList {
		names = append(names, name)
	}
	hookListMu.RUnlock()
	sort.Strings(names)
	return names
}

func Snapshot() []framework.EmailHook {
	hookListMu.RLock()
	hooks := make([]framework.EmailHook, 0, len(HookList))
	for _, hook := range HookList {
		if hook != nil {
			hooks = append(hooks, hook)
		}
	}
	hookListMu.RUnlock()
	return hooks
}

func RemoveHooksByPlugin(pluginName string) {
	hookListMu.Lock()
	for hookName, hook := range HookList {
		if sender, ok := hook.(*HookSender); ok && sender.name == pluginName {
			delete(HookList, hookName)
		}
	}
	hookListMu.Unlock()
}
