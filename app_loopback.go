// 环回通道的适配层：桌面端与浏览器扩展之间那条本地 HTTP/WebSocket 通道。
// 落在这里的都是「扩展交过来什么、桌面端怎么接」的翻译——站点排除、请求上下文整理、
// 把一次交接转成一次入队。与 App 其余部分分开：App 保留绑定、设置与系统外壳适配。
package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"sheep-get/internal/config"
	"sheep-get/internal/credentials"
	"sheep-get/internal/engine"
	"sheep-get/internal/intake"
	"sheep-get/internal/server"
	"sheep-get/internal/settingsapply"
	"sheep-get/internal/task"
	"sheep-get/internal/window"
)

// handoverHeaders 把扩展交过来的请求上下文整理成这次下载要用的请求头。
// 以扩展给的自定义头为底：Cookie 与 Referer 只在扩展没给同名头时补上，页面地址为空就不给。
// 交接、清晰度列表与媒体概览三条路径共用这一份整理，扩展交过来什么就带什么。
func handoverHeaders(creds *server.CredentialsPayload, referrer string) map[string]string {
	headers := make(map[string]string)
	if creds != nil {
		for name, value := range creds.Headers {
			headers[name] = value
		}
		if creds.Cookies != "" && headers["Cookie"] == "" {
			headers["Cookie"] = creds.Cookies
		}
	}
	if referrer != "" && headers["Referer"] == "" {
		headers["Referer"] = referrer
	}
	return headers
}

type loopbackServerAdapter struct {
	app *App
}

func (a *loopbackServerAdapter) HandleHandover(ctx context.Context, req *server.HandoverRequest) (*server.HandoverResponse, error) {
	return a.app.handleHandover(ctx, req)
}

func (a *loopbackServerAdapter) HandleHLSVariants(ctx context.Context, req *server.HLSVariantsRequest) (*server.HLSVariantsResponse, error) {
	return a.app.handleHLSVariants(ctx, req)
}

func (a *loopbackServerAdapter) HandleMediaProbe(ctx context.Context, req *server.MediaProbeRequest) (*server.MediaProbeResponse, error) {
	return a.app.handleMediaProbe(ctx, req)
}

func (a *loopbackServerAdapter) HandleCreateTask(ctx context.Context, req *server.CreateTaskRequest) (*server.CreateTaskResponse, error) {
	return a.app.handleCreateTask(ctx, req)
}

func (a *loopbackServerAdapter) GetTakeoverSync() server.TakeoverConfigSync {
	return a.app.getTakeoverSync()
}

// getTakeoverSync 是扩展主动来取配置时的那一份：与设置变更推给扩展的规则同源，
// 翻译只在 settingsapply 里做一次。
func (a *App) getTakeoverSync() server.TakeoverConfigSync {
	if a.settings == nil {
		return server.TakeoverConfigSync{}
	}
	st := a.settings.Get()
	return settingsapply.TakeoverSync(&st)
}

func (a *App) handleHandover(_ context.Context, req *server.HandoverRequest) (*server.HandoverResponse, error) {
	if a.windowQueue == nil {
		return &server.HandoverResponse{Accepted: false, Reason: "window queue not initialized"}, nil
	}

	st := a.GetSettings()
	if req.SourceType == "browser_takeover" && req.PageContext.PageURL != "" {
		if config.SiteMatchesExcluded(req.PageContext.PageURL, st.Takeover.ExcludedSites) {
			return &server.HandoverResponse{Accepted: false, Reason: "site_excluded"}, nil
		}
	}

	headers := handoverHeaders(req.Credentials, req.PageContext.Referrer)

	pageURL := req.PageContext.PageURL
	if pageURL == "" {
		pageURL = req.PageContext.Referrer
	}

	dlReq := window.DownloadRequest{
		URL:        req.URL,
		Filename:   req.FilenameSuggestion,
		Headers:    headers,
		VariantURI: req.VariantURI,
		PageURL:    pageURL,
	}
	resp, err := a.TriggerDownload(dlReq)
	if err != nil {
		return &server.HandoverResponse{Accepted: false, Reason: err.Error()}, nil
	}
	return &server.HandoverResponse{
		Accepted:    resp.Handled,
		QueueItemID: resp.RequestID,
	}, nil
}

func (a *App) handleCreateTask(ctx context.Context, req *server.CreateTaskRequest) (*server.CreateTaskResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}
	if strings.TrimSpace(req.ZipID) != "" {
		return a.registerBundleMember(ctx, req)
	}
	dlReq := window.DownloadRequest{
		URL:       req.URL,
		Filename:  req.Filename,
		Directory: req.Directory,
		Headers:   req.Headers,
	}
	return a.directDownload(ctx, dlReq, progressDisplayOverride(req))
}

// registerBundleMember 把一个成员登记进 zip 聚合批次。它不经过文件信息窗口，也不为
// 成员创建任务：落点、分类与展示约定只在首个成员时解析一次，后续成员沿用批次的事实。
func (a *App) registerBundleMember(ctx context.Context, req *server.CreateTaskRequest) (*server.CreateTaskResponse, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("download engine not initialized")
	}
	if a.settings == nil {
		return nil, fmt.Errorf("settings not initialized")
	}

	st := a.GetSettings()
	zipID := strings.TrimSpace(req.ZipID)
	zipName := engine.BundleFilename(req.ZipName, zipID)
	dir := strings.TrimSpace(req.Directory)
	cat, resolvedDir := st.Download.ResolveDestination(zipName)
	categoryID := cat.ID
	if dir == "" {
		dir = resolvedDir
	}

	reg, err := a.manager.RegisterBundleMember(ctx, engine.BundleRegisterRequest{
		ZipID:           zipID,
		ZipName:         req.ZipName,
		Directory:       dir,
		CategoryID:      categoryID,
		Final:           req.ZipFinal,
		MaxConn:         st.Download.DefaultConnectionsPerTask,
		ProgressDisplay: progressDisplayOverride(req),
		Member: engine.BundleMemberRequest{
			URL:       req.URL,
			Filename:  req.Filename,
			EntryPath: req.ZipPath,
			Headers:   req.Headers,
		},
	})
	if err != nil {
		return nil, err
	}

	// 只在创建批次时按设置呼出一次进度窗口；后续成员登记不再反复抢前台。
	if reg.Created {
		if show, _, _ := a.progressDisplay(reg.Task); show {
			a.ShowProgressWindow(reg.Task.ID)
		}
	}

	bundle := reg.Task.Bundle
	return &server.CreateTaskResponse{
		TaskID:    "",
		URL:       req.URL,
		Filename:  reg.Task.Filename,
		Directory: reg.Task.Directory,
		Status:    string(reg.Task.Status),
		ZipTaskID: reg.Task.ID,
		Zip: &server.ZipTaskState{
			ID:         bundle.ZipID,
			Name:       bundle.Name,
			Directory:  reg.Task.Directory,
			Files:      bundle.FilesTotal,
			KnownBytes: bundle.KnownBytes,
			TotalBytes: reg.Task.TotalBytes,
			Sealed:     bundle.Sealed,
			Status:     string(reg.Task.Status),
		},
		Member: &server.ZipMemberState{
			Filename:   reg.Member.Filename,
			Path:       reg.Member.EntryPath,
			TotalBytes: reg.Member.TotalBytes,
			Duplicate:  reg.Duplicate,
		},
	}, nil
}

// progressDisplayOverride 把接口按次传入的展示参数翻成任务上的覆盖。三项都没给时返回 nil，
// 表示完全跟随配置中心——未传参数的调用方看到的仍是从前那套行为。
func progressDisplayOverride(req *server.CreateTaskRequest) *task.ProgressDisplay {
	if req.ShowProgressWindow == nil && req.KeepCompletedInfo == nil && req.FocusProgressWindowOnComplete == nil {
		return nil
	}
	return &task.ProgressDisplay{
		ShowProgressWindow:            req.ShowProgressWindow,
		KeepCompletedInfo:             req.KeepCompletedInfo,
		FocusProgressWindowOnComplete: req.FocusProgressWindowOnComplete,
	}
}

// directDownload 直接创建并启动下载任务，跳过文件信息确认窗口，按设置呼起下载进度窗口。
//
// 它只做请求翻译：探测、命名、HLS、落点、占用与重复裁决都在 intake 里，与文件信息窗口
// 共用同一份实现。没有确认窗口可问，需要用户决定的局面由 intake 按安全答案落地。
// display 是这次请求的进度窗口展示覆盖（来自 HTTP 直连接口），为 nil 时跟随配置中心。
// 它不导出为 Wails 绑定——只被 loopback 服务器调用，导出只会把 server 包的类型泄漏进前端模型图。
func (a *App) directDownload(ctx context.Context, req window.DownloadRequest, display *task.ProgressDisplay) (*server.CreateTaskResponse, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("download engine not initialized")
	}
	if a.settings == nil {
		return nil, fmt.Errorf("settings not initialized")
	}
	urlStr := strings.TrimSpace(req.URL)
	if urlStr == "" {
		return nil, fmt.Errorf("missing download URL")
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL scheme (only http/https supported)")
	}

	downloads := intake.New(a.manager, a.settings)
	target := downloads.Begin(ctx, intake.Request{
		URL:             urlStr,
		Headers:         req.Headers,
		Filename:        req.Filename,
		Directory:       req.Directory,
		PageURL:         req.PageURL,
		VariantURI:      req.VariantURI,
		MaxConn:         req.MaxConn,
		Mode:            intake.ModeDirect,
		ProgressDisplay: display,
	}, nil)

	probe, probeErr := downloads.Probe(ctx, target)
	downloads.ApplyProbe(ctx, &target, probe, probeErr, nil)

	created, err := downloads.Submit(ctx, target, intake.Choice{})
	if err != nil {
		return nil, err
	}

	// 唤起进度窗口（跳过文件信息确认窗口）：这次请求自己说了算的那一项优先，
	// 没说就按配置中心。
	if show, _, _ := a.progressDisplay(created.Task); show {
		a.ShowProgressWindow(created.Task.ID)
	}

	return &server.CreateTaskResponse{
		TaskID:    created.Task.ID,
		URL:       created.Task.URL,
		Filename:  created.Task.Filename,
		Directory: created.Task.Directory,
		Status:    string(created.Task.Status),
	}, nil
}

// handleHLSVariants 读取一份清单的可选清晰度，供扩展悬浮条在交接前弹菜单。
// 请求上下文（Referer/Cookie）与交接走同一条整理路径：扩展交过来什么就带什么。
// 它不导出为 Wails 绑定——只被 loopback 服务器调用，绑定只会把 server 包的类型
// 泄漏进桌面前端的模型图里。
func (a *App) handleHLSVariants(ctx context.Context, req *server.HLSVariantsRequest) (*server.HLSVariantsResponse, error) {
	headers := handoverHeaders(req.Credentials, "")

	opts, err := a.manager.HLSVariantOptions(ctx, req.URL, credentials.New(headers))
	if err != nil {
		return nil, err
	}

	out := &server.HLSVariantsResponse{Variants: make([]server.HLSVariantOption, 0, len(opts))}
	for _, o := range opts {
		out.Variants = append(out.Variants, server.HLSVariantOption{
			URI:       o.URI,
			Label:     o.Label,
			Bandwidth: o.Bandwidth,
		})
	}
	return out, nil
}

// handleMediaProbe 为扩展面板探测一条链接的展示信息（时长与大小）。
// 请求上下文（Referer/Cookie）与交接走同一条整理路径：扩展交过来什么就带什么。
// 它不导出为 Wails 绑定——只被 loopback 服务器调用，导出只会把 server 包的类型
// 泄漏进桌面前端的模型图里。
func (a *App) handleMediaProbe(ctx context.Context, req *server.MediaProbeRequest) (*server.MediaProbeResponse, error) {
	headers := handoverHeaders(req.Credentials, "")

	ov, err := a.manager.ProbeMediaOverview(ctx, req.URL, req.Filename, req.MimeType, req.IsHls, req.TotalBytes, headers)
	if err != nil {
		return nil, err
	}
	return &server.MediaProbeResponse{
		DurationSeconds: ov.DurationSeconds,
		TotalBytes:      ov.TotalBytes,
		Variants:        ov.Variants,
	}, nil
}
