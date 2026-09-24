// 环回通道的适配层：桌面端与浏览器扩展之间那条本地 HTTP/WebSocket 通道。
// 落在这里的都是「扩展交过来什么、桌面端怎么接」的翻译——站点排除、请求上下文整理、
// 把一次交接转成一次入队。与 App 其余部分分开：App 保留绑定、设置与系统外壳适配。
package main

import (
	"context"

	"sheep-get/internal/config"
	"sheep-get/internal/credentials"
	"sheep-get/internal/server"
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

func (a *loopbackServerAdapter) GetTakeoverSync() server.TakeoverConfigSync {
	return a.app.getTakeoverSync()
}

func takeoverSyncFromSettings(st *config.Settings) server.TakeoverConfigSync {
	if st == nil {
		return server.TakeoverConfigSync{}
	}
	return server.TakeoverConfigSync{
		Extensions:    st.Download.AllExtensions(),
		ExcludedSites: st.Takeover.ExcludedSites,
		PauseShortcut: st.Takeover.PauseShortcut,
		ForceShortcut: st.Takeover.ForceShortcut,
	}
}

func (a *App) getTakeoverSync() server.TakeoverConfigSync {
	if a.settings == nil {
		return server.TakeoverConfigSync{}
	}
	st := a.settings.Get()
	return takeoverSyncFromSettings(&st)
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
