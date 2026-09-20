//go:build linux

package hooks

import "testing"

func TestPluginIdentityBoundsBeforeCredentialConversion(t *testing.T) {
	for _, name := range []string{"SMAIL_PLUGIN_UID", "SMAIL_PLUGIN_GID"} {
		for _, item := range []struct {
			value string
			want  int
		}{
			{"1", 1}, {"65534", 65534}, {"1000", 1000},
			{"", 0}, {"0", 0}, {"-1", 0}, {"65535", 0},
			{"4294967296", 0}, {"9223372036854775808", 0},
			{"abc", 0}, {"1.5", 0}, {" 1000", 0},
		} {
			t.Run(name+"/"+item.value, func(t *testing.T) {
				t.Setenv(name, item.value)
				got, err := pluginIdentity(name)
				if item.want == 0 {
					if err == nil || got != 0 {
						t.Fatal("无效插件身份未在整数转换前被拒绝")
					}
				} else if err != nil || got != item.want {
					t.Fatal("合法插件身份被拒绝或发生截断")
				}
			})
		}
	}
}
