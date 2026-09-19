import { useEffect, useRef, useState } from 'react';
import { watchDesktopStatus } from '../../lib/liveStatus';
import { formatBytes, formatDuration, mimeShortLabel, type MediaResource } from '../../lib/media';
import type { DesktopStatus, HandoverResponse, MediaProbeInfo } from '../../lib/types';

/** 读后台维护的链路状态；force 为真时要求先重新验证再回答。 */
function requestStatus(force: boolean): Promise<DesktopStatus | null> {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage(
      { type: force ? 'RECONNECT' : 'GET_STATUS' },
      (res?: DesktopStatus) => {
        resolve(res ?? null);
      },
    );
  });
}

function formatClock(ts: number | null): string {
  if (!ts) return '—';
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export default function App() {
  const [resources, setResources] = useState<MediaResource[]>([]);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<DesktopStatus | null>(null);
  const [checking, setChecking] = useState(true);
  const [handoverStates, setHandoverStates] = useState<
    Record<string, 'idle' | 'sending' | 'success' | 'failed'>
  >({});
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  /** 每条资源探测到的展示信息（时长与大小），按资源 id 存。 */
  const [probeInfo, setProbeInfo] = useState<Record<string, MediaProbeInfo>>({});
  /** 已经发起过探测的资源：探测失败也不重试，避免面板开着时反复打桌面端。 */
  const probedRef = useRef<Set<string>>(new Set());

  const online = status?.online === true;

  useEffect(() => {
    void loadMedia();
    void refreshLink(false);
    // 面板开着就一直盯着链路：桌面端中途退出、或者用户这会儿才启动它，状态都要跟着变，
    // 而不是停在打开面板那一刻的答案上。
    const watch = watchDesktopStatus({
      probe: () => requestStatus(true),
      onStatus: (next) => setStatus(next),
    });
    return watch.stop;
  }, []);

  // 链路在线时对还没探过的资源发起概览探测：时长与大小在下载之前就显示出来。
  // 后台预探测已经回填的资源（probe* 字段）直接用，不再发请求；探测本身命中
  // background 的 URL 级缓存，同一地址反复打开面板只算一次。失败静默——面板
  // 不为展示信息阻塞。
  useEffect(() => {
    if (!online) return;
    for (const res of resources) {
      if (probedRef.current.has(res.id)) continue;
      if (res.probeDuration || res.probeTotalBytes || res.probeVariants) continue;
      probedRef.current.add(res.id);
      chrome.runtime.sendMessage(
        {
          type: 'GET_MEDIA_PROBE',
          url: res.url,
          filename: res.filename,
          mimeType: res.mimeType,
          isHls: res.isHls,
          totalBytes: res.totalBytes,
          pageUrl: res.pageUrl,
        },
        (info: MediaProbeInfo | undefined) => {
          if (info && (info.durationSeconds || info.totalBytes || info.variants)) {
            setProbeInfo((prev) => ({ ...prev, [res.id]: info }));
          }
        },
      );
    }
  }, [online, resources]);

  /**
   * 连接状态一律问后台，不问本地存下来的会话：存着会话不代表连得上
   * （桌面端重启会换端口），面板显示「已就绪」却交接失败是最误导人的状态。
   * 打开面板时如果缓存显示离线，就直接做一次真实重连，把答案给出来而不是让用户猜。
   */
  async function refreshLink(force: boolean) {
    setChecking(true);
    try {
      const cached = await requestStatus(force);
      if (cached && !cached.online && !force) {
        setStatus(await requestStatus(true));
      } else {
        setStatus(cached);
      }
    } finally {
      setChecking(false);
    }
  }

  async function loadMedia() {
    setLoading(true);
    try {
      // Query active tab in current window
      const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
      const currentTab = tabs[0];
      if (currentTab?.id !== undefined) {
        chrome.runtime.sendMessage(
          { type: 'GET_TAB_MEDIA', tabId: currentTab.id },
          (response: MediaResource[] | undefined) => {
            if (response && Array.isArray(response)) {
              setResources(response);
            }
            setLoading(false);
          },
        );
      } else {
        setLoading(false);
      }
    } catch {
      setLoading(false);
    }
  }

  async function handleDownload(res: MediaResource) {
    setHandoverStates((prev) => ({ ...prev, [res.id]: 'sending' }));
    setErrorMessage(null);

    chrome.runtime.sendMessage(
      { type: 'HANDOVER_MEDIA', resource: res },
      (response: HandoverResponse | undefined) => {
        const runtimeError = chrome.runtime.lastError?.message;
        if (response?.accepted) {
          setHandoverStates((prev) => ({ ...prev, [res.id]: 'success' }));
          setTimeout(() => {
            setHandoverStates((prev) => ({ ...prev, [res.id]: 'idle' }));
          }, 3000);
        } else {
          setHandoverStates((prev) => ({ ...prev, [res.id]: 'failed' }));
          setErrorMessage(response?.reason || runtimeError || '移交失败');
        }
        // 交接失败往往意味着链路刚断，顺手把面板状态刷新到真实值。
        void refreshLink(false);
      },
    );
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        minHeight: '160px',
        maxHeight: '520px',
        backgroundColor: '#090d16',
        color: '#e2e8f0',
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        fontSize: '13px',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          borderBottom: '1px solid #1e293b',
          backgroundColor: '#0f172a',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span
            style={{
              fontWeight: 600,
              fontSize: '14px',
              letterSpacing: '-0.01em',
              color: '#f8fafc',
            }}
          >
            SheepGet
          </span>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
              fontSize: '11px',
              color: online ? '#10b981' : '#f59e0b',
              backgroundColor: online ? 'rgba(16, 185, 129, 0.1)' : 'rgba(245, 158, 11, 0.1)',
              padding: '2px 6px',
              borderRadius: '9999px',
            }}
          >
            <span
              style={{
                width: '6px',
                height: '6px',
                borderRadius: '50%',
                backgroundColor: online ? '#10b981' : '#f59e0b',
              }}
            />
            {checking ? '检查中…' : online ? '已连接' : '未连接'}
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
          <button
            type="button"
            onClick={() => void refreshLink(true)}
            disabled={checking}
            title="重新验证与桌面端的连接"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
              fontSize: '11px',
              padding: '2px 8px',
              backgroundColor: '#1e293b',
              color: checking ? '#64748b' : '#38bdf8',
              border: '1px solid #334155',
              borderRadius: '4px',
              cursor: checking ? 'default' : 'pointer',
            }}
          >
            <svg
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.4"
              strokeLinecap="round"
              strokeLinejoin="round"
              style={{ animation: checking ? 'sheepget-spin 0.9s linear infinite' : 'none' }}
            >
              <path d="M21 12a9 9 0 1 1-2.64-6.36" />
              <path d="M21 3v6h-6" />
            </svg>
            {checking ? '连接中…' : '刷新连接'}
          </button>
          <span
            style={{
              fontSize: '11px',
              color: '#64748b',
              backgroundColor: '#1e293b',
              padding: '2px 8px',
              borderRadius: '4px',
            }}
          >
            {resources.length} 个资源
          </span>
        </div>
      </div>

      {/* Disconnected notification banner */}
      {!online && !checking && (
        <div
          style={{
            padding: '10px 14px',
            backgroundColor: 'rgba(30, 41, 59, 0.7)',
            borderBottom: '1px solid #1e293b',
            display: 'flex',
            flexDirection: 'column',
            gap: '6px',
            fontSize: '11px',
          }}
        >
          <span style={{ color: '#fbbf24' }}>
            桌面端连接不可用：{status?.reason ?? '未检测到运行中的桌面端'}
          </span>
          <span style={{ color: '#64748b' }}>此时浏览器会自行下载，不受接管设置影响。</span>
        </div>
      )}

      {/* Error alert if any */}
      {errorMessage && (
        <div
          style={{
            padding: '8px 16px',
            backgroundColor: 'rgba(239, 68, 68, 0.15)',
            borderBottom: '1px solid rgba(239, 68, 68, 0.3)',
            color: '#fca5a5',
            fontSize: '12px',
          }}
        >
          {errorMessage}
        </div>
      )}

      {/* Content list */}
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '8px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
        }}
      >
        {loading ? (
          <div
            style={{
              padding: '32px 16px',
              textAlign: 'center',
              color: '#64748b',
              fontSize: '12px',
            }}
          >
            正在探测页面媒体...
          </div>
        ) : resources.length === 0 ? (
          <div
            style={{
              padding: '40px 16px',
              textAlign: 'center',
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: '6px',
            }}
          >
            <span style={{ fontSize: '13px', color: '#94a3b8' }}>未探测到媒体资源</span>
            <span style={{ fontSize: '11px', color: '#64748b' }}>
              在页面中播放音视频或刷新页面后重新探测
            </span>
          </div>
        ) : (
          resources.map((item) => {
            const state = handoverStates[item.id] || 'idle';
            // 探测信息两路合并：面板自己探测到的优先，其次后台预探测回填到资源上的。
            // HLS 的响应头大小只是清单本身的大小（几 KB），没有意义，真实总大小要等
            // 桌面端逐分片问出来；时长同理，探到了才显示。
            const info = probeInfo[item.id];
            const durationText = formatDuration(info?.durationSeconds ?? item.probeDuration);
            const sizeText = formatBytes(
              info?.totalBytes ?? item.probeTotalBytes ?? item.totalBytes,
            );
            const variantCount = info?.variants ?? item.probeVariants;
            const variantsText = variantCount && variantCount > 1 ? `${variantCount} 个清晰度` : '';
            const mimeText = mimeShortLabel(item.mimeType);
            return (
              <div
                key={item.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '10px 12px',
                  backgroundColor: '#0f172a',
                  borderRadius: '6px',
                  border: '1px solid #1e293b',
                  gap: '12px',
                }}
              >
                {/* Media icon & info */}
                <div
                  style={{
                    flex: 1,
                    minWidth: 0,
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '4px',
                  }}
                >
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '6px',
                    }}
                  >
                    <span
                      style={{
                        fontSize: '10px',
                        fontWeight: 600,
                        padding: '1px 5px',
                        borderRadius: '3px',
                        backgroundColor: item.isHls
                          ? 'rgba(168, 85, 247, 0.2)'
                          : 'rgba(16, 185, 129, 0.2)',
                        color: item.isHls ? '#c084fc' : '#34d399',
                        textTransform: 'uppercase',
                      }}
                    >
                      {item.isHls ? 'HLS' : '直链'}
                    </span>
                    <span
                      title={item.filename}
                      style={{
                        fontSize: '12px',
                        fontWeight: 500,
                        color: '#f1f5f9',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {item.filename}
                    </span>
                  </div>

                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      fontSize: '11px',
                      color: '#64748b',
                      whiteSpace: 'nowrap',
                      minWidth: 0,
                    }}
                  >
                    {variantsText ? (
                      <span style={{ color: '#c084fc', flexShrink: 0 }}>{variantsText}</span>
                    ) : (
                      <>
                        {durationText && (
                          <span style={{ color: '#94a3b8', flexShrink: 0 }}>{durationText}</span>
                        )}
                        {durationText && <span style={{ flexShrink: 0 }}>•</span>}
                        <span style={{ flexShrink: 0 }}>{sizeText}</span>
                      </>
                    )}
                    <span style={{ flexShrink: 0 }}>•</span>
                    <span
                      title={item.mimeType}
                      style={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        flex: 1,
                        minWidth: 0,
                      }}
                    >
                      {mimeText}
                    </span>
                  </div>
                </div>

                {/* Action button */}
                <button
                  type="button"
                  onClick={() => handleDownload(item)}
                  disabled={state === 'sending' || state === 'success'}
                  style={{
                    padding: '6px 12px',
                    fontSize: '11px',
                    fontWeight: 500,
                    borderRadius: '4px',
                    cursor: state === 'sending' || state === 'success' ? 'default' : 'pointer',
                    border: 'none',
                    backgroundColor:
                      state === 'success' ? '#059669' : state === 'failed' ? '#dc2626' : '#10b981',
                    color: '#ffffff',
                    transition: 'background-color 0.15s ease',
                    flexShrink: 0,
                  }}
                >
                  {state === 'sending'
                    ? '移交中...'
                    : state === 'success'
                      ? '已移交'
                      : state === 'failed'
                        ? '重试'
                        : '下载'}
                </button>
              </div>
            );
          })
        )}
      </div>

      {/* Footer info */}
      <div
        style={{
          padding: '8px 16px',
          borderTop: '1px solid #1e293b',
          backgroundColor: '#0b1120',
          fontSize: '11px',
          color: '#475569',
          display: 'flex',
          justifyContent: 'space-between',
        }}
      >
        <span>
          {online
            ? `本地端口 ${status?.port} · 上次校验 ${formatClock(status?.lastVerifiedAt ?? null)}`
            : '桌面端离线 (未启动或已退出)'}
        </span>
        <span style={{ color: '#64748b' }}>v1.0.0</span>
      </div>
    </div>
  );
}
