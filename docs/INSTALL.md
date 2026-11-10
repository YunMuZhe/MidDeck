# 安装说明 / Installation

MidDeck 的安装包目前**没有代码签名**。这不影响使用，只是首次打开时系统会弹出安全提示，按下面的方法处理一次即可。

## macOS

1. 下载 `MidDeck-*-macos-universal.dmg`，打开后把 MidDeck 拖进“应用程序”。
2. 第一次双击打开，系统会提示“无法验证开发者”，点“完成”或“取消”。
3. 放行：
   - **macOS 15 (Sequoia) 及以后**：打开“系统设置 → 隐私与安全性”，滚到底部，点 MidDeck 旁边的“仍要打开”，输入密码。
   - **macOS 14 及以前**：在“应用程序”中右键 MidDeck → 打开 → 打开。

也可以用一条命令直接去掉隔离标记：

```bash
xattr -dr com.apple.quarantine /Applications/MidDeck.app
```

## Windows

运行 `setup.exe` 或 `portable.exe` 时，如果出现“Windows 已保护你的电脑”（SmartScreen），点 **更多信息 → 仍要运行**。

MidDeck 的界面依赖 WebView2 运行时。Windows 11 和较新的 Windows 10 已经自带；Windows Server 等没有自带的机器上，`setup.exe` 和 `portable.exe` 会提示联网下载。

**离线机器（如堡垒机）**请改用 `MidDeck-*-windows-*-offline.zip`：

1. 解压到任意目录（例如 `D:\MidDeck`），保持 `MidDeck.exe` 和 `webview2\` 文件夹在同一层。
2. 双击 `MidDeck.exe`。不需要安装、管理员权限或联网。

首次启动时，MidDeck 会给 `webview2\` 目录授予 WebView2 沙箱所需的读取权限（等价于下面两条命令）。如果窗口白屏，可以手动执行一次：

```bat
icacls webview2 /grant *S-1-15-2-1:(OI)(CI)(RX) /T
icacls webview2 /grant *S-1-15-2-2:(OI)(CI)(RX) /T
```

WebView2 仅支持 Windows 10 / Server 2016 及以上。

## Linux

```bash
# Debian / Ubuntu
sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
tar xzf MidDeck-*-linux-amd64.tar.gz
./MidDeck
```

## 数据存放位置

| 内容 | 位置 |
|---|---|
| 实例配置 | macOS `~/Library/Application Support/MidDeck/`，Windows `%AppData%\MidDeck\`，Linux `~/.config/MidDeck/` |
| 密码、口令 | 系统钥匙串（服务名 `MidDeck`）。系统不支持钥匙串时退回到配置文件 |
| 审计日志 | 同配置目录下的 `audit.log` |
| SSH 主机指纹 | 同配置目录下的 `known_hosts`（首次连接时记录） |

---

## English

Builds are not code-signed yet.

- **macOS 15+**: open the app once, then go to *System Settings → Privacy & Security* and click **Open Anyway**. On macOS 14 and earlier, right-click the app → Open. Or run `xattr -dr com.apple.quarantine /Applications/MidDeck.app`.
- **Windows**: on the SmartScreen dialog click **More info → Run anyway**. On machines without WebView2 or internet (e.g. bastion hosts), use `MidDeck-*-windows-*-offline.zip`: unzip it, keep `MidDeck.exe` next to the `webview2\` folder, and run `MidDeck.exe`.
- **Linux**: install `libwebkit2gtk-4.1-0` and `libgtk-3-0`, then run the binary.
