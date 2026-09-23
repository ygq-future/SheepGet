// Package browser 提供浏览器可执行文件探测与配套浏览器扩展目录解析能力。
package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 扩展目录的布局：
// 1. 分发包把扩展放在 exe 同级的 extension/
// 2. 源码构建产物（bun run --cwd extension build）输出到 build/dist-extension/chrome-mv3
// 3. 同时兼容旧路径 dist-extension/chrome-mv3
const (
	ExtensionDirName = "extension"
	BuildVariantDir  = "chrome-mv3"
)

// ExtensionInstallPlan 是一次扩展页交接的全部要素：
// 需要写进剪贴板的特权地址，以及唤起浏览器的命令。
type ExtensionInstallPlan struct {
	Address string
	Exe     string
	Args    []string
}

// ResolveExtensionDir searches candidate locations for the browser extension directory
// containing a valid manifest.json file.
func ResolveExtensionDir(execDir, workingDir string) (string, error) {
	var candidates []string
	if execDir != "" {
		candidates = append(candidates, filepath.Join(execDir, ExtensionDirName))
	}
	if workingDir != "" {
		candidates = append(candidates,
			filepath.Join(workingDir, ExtensionDirName),
			filepath.Join(workingDir, "build", "dist-extension", BuildVariantDir),
			filepath.Join(workingDir, "dist-extension", BuildVariantDir),
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

// PlanExtensionInstall 组装浏览器扩展管理页的交接方案。
func PlanExtensionInstall(goos, browser, exePath string) (ExtensionInstallPlan, error) {
	normalized := strings.ToLower(strings.TrimSpace(browser))
	var address string
	switch normalized {
	case "chrome":
		address = "chrome://extensions"
	case "edge":
		address = "edge://extensions"
	default:
		return ExtensionInstallPlan{}, fmt.Errorf("不支持的浏览器: %s", browser)
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
		return ExtensionInstallPlan{}, fmt.Errorf("不支持的系统平台: %s", goos)
	}

	return ExtensionInstallPlan{Address: address, Exe: exe, Args: args}, nil
}

// ExtensionDirectory resolves the absolute directory path of the bundled browser extension.
func ExtensionDirectory() (string, error) {
	var execDir string
	if exe, err := os.Executable(); err == nil {
		execDir = filepath.Dir(exe)
	}
	cwd, _ := os.Getwd()
	return ResolveExtensionDir(execDir, cwd)
}
