package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/YunMuZhe/MidDeck/internal/execx"
)

// bundledWebviewDir is the folder, next to MidDeck.exe, that the offline package
// ships a WebView2 Fixed Version runtime in. When present it is used instead of
// the system-wide runtime, so machines without internet (bastion hosts) never hit
// the "WebView2 runtime is required, download?" prompt.
const bundledWebviewDir = "webview2"

func windowsOptions() *windows.Options {
	opts := &windows.Options{}
	exe, err := os.Executable()
	if err != nil {
		return opts
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return opts
	}
	dir := filepath.Join(filepath.Dir(exe), bundledWebviewDir)
	if _, err := os.Stat(filepath.Join(dir, "msedgewebview2.exe")); err != nil {
		return opts
	}
	grantSandboxAccess(dir)
	opts.WebviewBrowserPath = dir
	return opts
}

// grantSandboxAccess gives the AppContainer SIDs read/execute on the runtime folder.
// Fixed Version 120+ renderers run sandboxed and crash (blank window) on Windows 10 /
// Server when the folder sits somewhere those SIDs can't read, e.g. under the user
// profile where a zip is usually extracted. Done once; a marker file records it.
func grantSandboxAccess(dir string) {
	marker := filepath.Join(dir, ".middeck-acl")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ok := true
	for _, sid := range []string{"*S-1-15-2-1", "*S-1-15-2-2"} {
		cmd := execx.Command(ctx, "icacls", dir, "/grant", sid+":(OI)(CI)(RX)", "/T", "/Q", "/C")
		if err := cmd.Run(); err != nil {
			ok = false
		}
	}
	if ok {
		_ = os.WriteFile(marker, nil, 0o644)
	}
}
