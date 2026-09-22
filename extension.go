package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 扩展目录的两种布局：分发包把扩展放在 exe 同级的 extension/，
// 源码构建产物（bun run --cwd extension build）输出到仓库根的 dist-extension/chrome-mv3。
const (
	extensionDirName = "extension"
	buildOutputDir   = "dist-extension"
	buildVariantDir  = "chrome-mv3"
)

// resolveExtensionDir searches candidate locations for the browser extension directory
// containing a valid manifest.json file.
func resolveExtensionDir(execDir, workingDir string) (string, error) {
	var candidates []string
	if execDir != "" {
		candidates = append(candidates, filepath.Join(execDir, extensionDirName))
	}
	if workingDir != "" {
		candidates = append(candidates,
			filepath.Join(workingDir, extensionDirName),
			filepath.Join(workingDir, buildOutputDir, buildVariantDir),
		)
	}

	for _, cand := range candidates {
		manifestPath := filepath.Join(cand, "manifest.json")
		if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(cand)
			if err == nil {
				return abs, nil
			}
			return cand, nil
		}
	}

	return "", fmt.Errorf("配套浏览器扩展目录未找到（未在候选路径中检索到包含 manifest.json 的扩展文件夹）")
}

// extensionInstallPlan 是一次扩展页交接的全部要素：
// 需要写进剪贴板的特权地址，以及唤起浏览器的命令。
//
// Chromium 会丢弃命令行传入的 chrome:// 、edge:// 地址，也禁止网页内容导航过去，
// 所以特权地址只能靠用户粘贴进地址栏，命令行里只负责把浏览器唤到前台。
type extensionInstallPlan struct {
	address string
	exe     string
	args    []string
}

// planExtensionInstall 组装浏览器扩展管理页的交接方案。
func planExtensionInstall(goos, browser, exePath string) (extensionInstallPlan, error) {
	normalized := strings.ToLower(strings.TrimSpace(browser))
	var address string
	switch normalized {
	case "chrome":
		address = "chrome://extensions"
	case "edge":
		address = "edge://extensions"
	default:
		return extensionInstallPlan{}, fmt.Errorf("不支持的浏览器: %s", browser)
	}
	isEdge := normalized == "edge"

	var exe string
	var args []string
	switch goos {
	case "windows":
		switch {
		case exePath != "":
			exe = exePath
		case isEdge:
			exe, args = "cmd", []string{"/c", "start", "", "msedge"}
		default:
			exe, args = "cmd", []string{"/c", "start", "", "chrome"}
		}
	case "darwin":
		appName := "Google Chrome"
		if isEdge {
			appName = "Microsoft Edge"
		}
		exe, args = "open", []string{"-a", appName}
	case "linux":
		exe = "google-chrome"
		if isEdge {
			exe = "microsoft-edge"
		}
	default:
		return extensionInstallPlan{}, fmt.Errorf("不支持的系统平台: %s", goos)
	}

	return extensionInstallPlan{address: address, exe: exe, args: args}, nil
}

// extensionDirectory resolves the absolute directory path of the bundled browser extension.
func extensionDirectory() (string, error) {
	var execDir string
	if exe, err := os.Executable(); err == nil {
		execDir = filepath.Dir(exe)
	}
	cwd, _ := os.Getwd()
	return resolveExtensionDir(execDir, cwd)
}

// OpenExtensionFolder reveals the bundled extension directory in the system file manager.
func (a *App) OpenExtensionFolder() error {
	dir, err := extensionDirectory()
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", dir)
	case "darwin":
		cmd = exec.Command("open", "-R", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}

	return cmd.Start()
}

// PrepareExtensionPage 唤起浏览器并把扩展管理页地址写入剪贴板，返回该地址。
// 用户在浏览器地址栏粘贴即可进入扩展管理页，随后手动加载扩展目录。
func (a *App) PrepareExtensionPage(browser string) (string, error) {
	var exePath string
	if runtime.GOOS == "windows" {
		exePath = findBrowserExe(browser)
	}
	plan, err := planExtensionInstall(runtime.GOOS, browser, exePath)
	if err != nil {
		return "", err
	}

	app := a.getApp()
	if app == nil || app.Clipboard == nil || !app.Clipboard.SetText(plan.address) {
		return "", fmt.Errorf("写入剪贴板失败")
	}

	if err := exec.Command(plan.exe, plan.args...).Start(); err != nil {
		return "", fmt.Errorf("唤起浏览器失败: %w", err)
	}

	return plan.address, nil
}
