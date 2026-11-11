package ai

import "golang.org/x/sys/windows/registry"

func init() {
	readSystemProxy = func() (sysProxy, bool) {
		k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
		if err != nil {
			return sysProxy{}, false
		}
		defer k.Close()
		if on, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || on == 0 {
			return sysProxy{}, false
		}
		server, _, _ := k.GetStringValue("ProxyServer")
		override, _, _ := k.GetStringValue("ProxyOverride")
		return parseWindowsProxy(server, override), true
	}
}
