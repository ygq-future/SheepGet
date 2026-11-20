package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"sheep-get/internal/credentials"
	"sheep-get/internal/media"
	"sheep-get/internal/task"
)

const (
	pairedVideoFilename = "video.m4s"
	pairedAudioFilename = "audio.m4s"
)

// pairedSink 将单路流的下载快照汇总后转发给主任务的 TransferSink，确保整个传输阶段使用
// 视频+音频两路之和作为总大小并透传分块状态，既避免进度条反弹也完整支持多线程分块展示。
type pairedSink struct {
	parent       TransferSink
	mainTask     *task.Task
	baseBytes    *atomic.Int64
	otherTotal   int64
	chunkOffset  int64
	prefixChunks []task.Chunk
	currentBytes atomic.Int64
}

func (s *pairedSink) WantsSnapshot() bool {
	return s.parent.WantsSnapshot()
}

func (s *pairedSink) PublishSnapshot(sub *task.Task) {
	if sub == nil {
		return
	}
	s.currentBytes.Store(sub.Downloaded)
	s.mainTask.Downloaded = s.baseBytes.Load() + sub.Downloaded
	if sub.TotalBytes > 0 && s.otherTotal > 0 {
		total := sub.TotalBytes + s.otherTotal
		if total > s.mainTask.TotalBytes {
			s.mainTask.TotalBytes = total
		}
	}

	var mergedChunks []task.Chunk
	if len(s.prefixChunks) > 0 || len(sub.Chunks) > 0 {
		mergedChunks = make([]task.Chunk, 0, len(s.prefixChunks)+len(sub.Chunks))
		mergedChunks = append(mergedChunks, s.prefixChunks...)
		indexOffset := len(s.prefixChunks)
		for _, c := range sub.Chunks {
			c.Index += indexOffset
			c.Start += s.chunkOffset
			c.End += s.chunkOffset
			mergedChunks = append(mergedChunks, c)
		}
		s.mainTask.Chunks = mergedChunks
	}

	snap := *s.mainTask
	snap.Speed = sub.Speed
	s.parent.PublishSnapshot(&snap)
}

type pairedStreamProbe struct {
	total     int64
	resumable bool
}

func (m *Manager) probePairedStreams(ctx context.Context, t *task.Task) (video, audio pairedStreamProbe) {
	creds := credentials.New(t.RequestHeaders)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if p, err := m.probeResource(ctx, t.URL, creds); err == nil && p != nil {
			if p.TotalBytes > 0 {
				video.total = p.TotalBytes
			}
			video.resumable = p.Resumable
		}
	}()
	go func() {
		defer wg.Done()
		if p, err := m.probeResource(ctx, t.AudioURL, creds); err == nil && p != nil {
			if p.TotalBytes > 0 {
				audio.total = p.TotalBytes
			}
			audio.resumable = p.Resumable
		}
	}()
	wg.Wait()
	return video, audio
}

// runPairedMediaTransfer 分别下载音视频分离流（如 B 站 DASH .m4s）到任务分片目录，
// 下载完成后交由 runPairedMediaProcessing 无损合并为 .mp4。
func (m *Manager) runPairedMediaTransfer(ctx context.Context, t *task.Task, sink TransferSink) error {
	segDir := m.segmentDir(t)
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		return fmt.Errorf("无法创建媒体临时目录: %w", err)
	}

	videoProbe, audioProbe := m.probePairedStreams(ctx, t)
	if videoProbe.total > 0 && audioProbe.total > 0 {
		t.TotalBytes = videoProbe.total + audioProbe.total
	} else {
		// 若任一路总大小尚未探明，清掉之前可能误用的单流 TotalBytes，避免先走到 100% 再反弹。
		t.TotalBytes = -1
	}

	var completedBytes atomic.Int64

	videoTask := &task.Task{
		ID:             t.ID,
		URL:            t.URL,
		Directory:      segDir,
		Filename:       pairedVideoFilename,
		TotalBytes:     videoProbe.total,
		Resumable:      videoProbe.resumable,
		MaxConcurrency: t.MaxConcurrency,
		DirectTransfer: t.DirectTransfer,
		ProxyAddr:      t.ProxyAddr,
		RequestHeaders: t.RequestHeaders,
	}
	vSink := &pairedSink{
		parent:     sink,
		mainTask:   t,
		baseBytes:  &completedBytes,
		otherTotal: audioProbe.total,
	}
	if err := m.downloader.Download(ctx, videoTask, vSink); err != nil {
		return fmt.Errorf("下载视频流失败: %w", err)
	}
	videoDone := videoTask.Downloaded
	if videoDone == 0 {
		videoDone = vSink.currentBytes.Load()
	}
	completedBytes.Store(videoDone)

	resolvedVideoTotal := videoTask.TotalBytes
	if resolvedVideoTotal <= 0 {
		resolvedVideoTotal = videoDone
	}

	videoChunks := append([]task.Chunk(nil), videoTask.Chunks...)
	if len(videoChunks) == 0 && videoDone > 0 {
		videoChunks = []task.Chunk{{
			Index:      0,
			Start:      0,
			End:        videoDone - 1,
			Downloaded: videoDone,
			Completed:  true,
		}}
	}

	audioTask := &task.Task{
		ID:             t.ID,
		URL:            t.AudioURL,
		Directory:      segDir,
		Filename:       pairedAudioFilename,
		TotalBytes:     audioProbe.total,
		Resumable:      audioProbe.resumable,
		MaxConcurrency: t.MaxConcurrency,
		DirectTransfer: t.DirectTransfer,
		ProxyAddr:      t.ProxyAddr,
		RequestHeaders: t.RequestHeaders,
	}
	aSink := &pairedSink{
		parent:       sink,
		mainTask:     t,
		baseBytes:    &completedBytes,
		otherTotal:   resolvedVideoTotal,
		chunkOffset:  resolvedVideoTotal,
		prefixChunks: videoChunks,
	}
	if err := m.downloader.Download(ctx, audioTask, aSink); err != nil {
		return fmt.Errorf("下载音频流失败: %w", err)
	}
	audioDone := audioTask.Downloaded
	if audioDone == 0 {
		audioDone = aSink.currentBytes.Load()
	}
	t.Downloaded = videoDone + audioDone
	if resolvedVideoTotal > 0 && audioTask.TotalBytes > 0 {
		t.TotalBytes = resolvedVideoTotal + audioTask.TotalBytes
	} else {
		t.TotalBytes = t.Downloaded
	}
	return nil
}

func (m *Manager) runPairedMediaProcessing(ctx context.Context, t *task.Task) error {
	if err := os.MkdirAll(t.Directory, 0o755); err != nil {
		return fmt.Errorf("无法创建保存目录: %w", err)
	}
	segDir := m.segmentDir(t)
	videoPath := filepath.Join(segDir, pairedVideoFilename)
	audioPath := filepath.Join(segDir, pairedAudioFilename)
	workDir := filepath.Join(segDir, "work")

	return media.Process(ctx, media.Input{
		Streams: []string{videoPath, audioPath},
		Output:  filepath.Join(t.Directory, t.Filename),
		WorkDir: workDir,
	})
}
