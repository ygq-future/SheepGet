import { useEffect, useState } from 'react';
import { formatBytes, type MediaResource } from '../../lib/media';
import { getStoredSession } from '../../lib/storage';
import type { HandoverResponse, SessionMetadata } from '../../lib/types';

export default function App() {
  const [resources, setResources] = useState<MediaResource[]>([]);
  const [loading, setLoading] = useState(true);
  const [session, setSession] = useState<SessionMetadata | null>(null);
  const [handoverStates, setHandoverStates] = useState<
    Record<string, 'idle' | 'sending' | 'success' | 'failed'>
  >({});
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    void loadData();
  }, []);

  async function loadData() {
    setLoading(true);
    try {
      const storedSession = await getStoredSession();
      setSession(storedSession);

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
        if (response?.accepted) {
          setHandoverStates((prev) => ({ ...prev, [res.id]: 'success' }));
          setTimeout(() => {
            setHandoverStates((prev) => ({ ...prev, [res.id]: 'idle' }));
          }, 3000);
        } else {
          setHandoverStates((prev) => ({ ...prev, [res.id]: 'failed' }));
          setErrorMessage(response?.reason || '移交失败');
        }
      },
    );
  }

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '380px',
        maxHeight: '480px',
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
              color: session ? '#10b981' : '#94a3b8',
              backgroundColor: session ? 'rgba(16, 185, 129, 0.1)' : 'rgba(148, 163, 184, 0.1)',
              padding: '2px 6px',
              borderRadius: '9999px',
            }}
          >
            <span
              style={{
                width: '6px',
                height: '6px',
                borderRadius: '50%',
                backgroundColor: session ? '#10b981' : '#94a3b8',
              }}
            />
            {session ? '已就绪' : '未连接'}
          </span>
        </div>
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
                    }}
                  >
                    <span>{formatBytes(item.totalBytes)}</span>
                    <span>•</span>
                    <span
                      style={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {item.mimeType}
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
        <span>{session ? `本地端口: ${session.port}` : '桌面端离线 (未启动)'}</span>
        <span style={{ color: '#64748b' }}>v1.0.0</span>
      </div>
    </div>
  );
}
