// Package notify shows a desktop notification using what the OS ships with:
// osascript on macOS, notify-send on Linux, a PowerShell toast on Windows.
package notify

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/execx"
)

// Send shows a notification. It returns an error when the OS helper is missing
// or fails; callers treat that as "show it in the app only".
func Send(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	title, body = oneLine(title, 120), oneLine(body, 400)
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %s with title "MidDeck" subtitle %s sound name "Submarine"`, appleQuote(body), appleQuote(title))
		return run(ctx, "osascript", "-e", script)
	case "linux":
		return run(ctx, "notify-send", "--app-name=MidDeck", "--urgency=critical", "--", title, body)
	case "windows":
		return run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", windowsToast(title, body))
	}
	return errors.New("notifications are not supported on " + runtime.GOOS)
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := execx.Command(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

// appleQuote makes an AppleScript string literal.
func appleQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// windowsToast builds a script that shows a toast through the WinRT API. Text is
// passed as single-quoted PowerShell literals and XML-escaped by the script.
func windowsToast(title, body string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	return `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null;` +
		`[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null;` +
		`$t = [System.Security.SecurityElement]::Escape(` + q(title) + `); $b = [System.Security.SecurityElement]::Escape(` + q(body) + `);` +
		`$x = New-Object Windows.Data.Xml.Dom.XmlDocument;` +
		`$x.LoadXml("<toast><visual><binding template='ToastGeneric'><text>$t</text><text>$b</text></binding></visual></toast>");` +
		`$n = [Windows.UI.Notifications.ToastNotification]::new($x);` +
		`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('MidDeck').Show($n)`
}
