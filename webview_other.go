//go:build !windows

package main

import "github.com/wailsapp/wails/v2/pkg/options/windows"

func windowsOptions() *windows.Options { return nil }
