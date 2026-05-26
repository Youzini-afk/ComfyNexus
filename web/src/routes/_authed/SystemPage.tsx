import { useEffect, useRef, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Cpu,
  RefreshCw,
  Loader2,
  Power,
  PowerOff,
  RotateCcw,
  Radio,
  Pause,
  TerminalSquare,
  Thermometer,
  Bolt,
  Monitor,
  Activity,
  HardDrive,
  AlertCircle,
  FileText,
} from 'lucide-react';
import { get, post, ApiError } from '@/lib/api';
import { useToasts } from '@/hooks/use-toasts';

/* =========================== Types ============================= */

type GPU = {
  index: number;
  name: string;
  uuid: string;
  utilizationGpu: number;
  utilizationMemory: number;
  memoryTotalMiB: number;
  memoryUsedMiB: number;
  temperatureC: number;
  powerDrawW: number;
  powerLimitW: number;
  driverVersion?: string;
  cudaVersion?: string;
};

type GPUStatus = {
  gpus: GPU[];
  raw?: string;
  updatedAt: string;
};

type ComfyStatus = {
  running: boolean;
  pids: number[] | null;
  port: number | null;
  root: string | null;
  updatedAt: string;
};

type LogsResponse = {
  path: string;
  text: string;
  updatedAt: string;
};

/* =========================== Helpers ============================= */

function fmtMiB(n: number): string {
  if (n === 0) return '0 MiB';
  if (n >= 1024) return `${(n / 1024).toFixed(2)} GiB`;
  return `${n} MiB`;
}

function percentColor(v: number): 'ok' | 'warn' | 'err' {
  if (v >= 90) return 'err';
  if (v >= 70) return 'warn';
  return 'ok';
}

function temperatureColor(v: number): 'ok' | 'warn' | 'err' {
  if (v >= 85) return 'err';
  if (v >= 70) return 'warn';
  return 'ok';
}

/* =========================== Components ============================= */

export function SystemPage() {
  const { t } = useTranslation(['system', 'common', 'errors']);
  const { toasts, show, remove } = useToasts();

  const [gpuData, setGpuData] = useState<GPUStatus | null>(null);
  const [gpuLoading, setGpuLoading] = useState(false);
  const [gpuError, setGpuError] = useState<string | null>(null);

  const [comfyData, setComfyData] = useState<ComfyStatus | null>(null);
  const [comfyLoading, setComfyLoading] = useState(false);
  const [restartLoading, setRestartLoading] = useState(false);

  const [logsText, setLogsText] = useState<string | null>(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState<string | null>(null);
  const [autoStream, setAutoStream] = useState(false);
  const [streamConnected, setStreamConnected] = useState(false);
  const logScrollRef = useRef<HTMLDivElement>(null);
  const shouldScrollRef = useRef(true);
  const esRef = useRef<EventSource | null>(null);

  /* Poll GPU and ComfyUI status every 5s */
  const fetchGPU = useCallback(async () => {
    setGpuLoading(true);
    setGpuError(null);
    try {
      const d = await get<GPUStatus>('/api/system/gpu');
      setGpuData(d);
    } catch (e) {
      setGpuError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setGpuLoading(false);
    }
  }, []);

  const fetchComfy = useCallback(async () => {
    setComfyLoading(true);
    try {
      const d = await get<ComfyStatus>('/api/system/comfyui/status');
      setComfyData(d);
    } catch (e) {
      // silently show empty state
      setComfyData(null);
    } finally {
      setComfyLoading(false);
    }
  }, []);

  const fetchLogs = useCallback(async () => {
    if (esRef.current && esRef.current.readyState !== EventSource.CLOSED) {
      // If we are live-streaming, no need to fetch static lines
      return;
    }
    setLogsLoading(true);
    setLogsError(null);
    try {
      const d = await get<LogsResponse>('/api/system/comfyui/logs?lines=200');
      setLogsText(d.text);
    } catch (e) {
      setLogsError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setLogsLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchGPU();
    void fetchComfy();
    void fetchLogs();
    const id = window.setInterval(() => {
      void fetchGPU();
      void fetchComfy();
    }, 5000);
    return () => window.clearInterval(id);
  }, [fetchGPU, fetchComfy, fetchLogs]);

  /* EventSource log stream */
  useEffect(() => {
    if (!autoStream) {
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
      setStreamConnected(false);
      return;
    }
    const es = new EventSource('/api/system/comfyui/logs/stream', {
      withCredentials: true,
    });
    esRef.current = es;

    es.addEventListener('ready', () => {
      setStreamConnected(true);
    });

    es.addEventListener('log', (e: MessageEvent) => {
      setLogsText((prev) => {
        const next = prev ? prev + '\n' + e.data : e.data;
        // Keep last ~500 lines to avoid memory bloat
        const lines = next.split('\n');
        if (lines.length > 500) return lines.slice(lines.length - 500).join('\n');
        return next;
      });
    });

    es.addEventListener('error', () => {
      setStreamConnected(false);
      show(t('system:logs.error'), 'err');
      es.close();
      esRef.current = null;
    });

    es.onopen = () => setStreamConnected(true);
    es.onerror = () => setStreamConnected(false);

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [autoStream, t, show]);

  /* Auto-scroll log panel when near bottom */
  useEffect(() => {
    if (logScrollRef.current && shouldScrollRef.current) {
      logScrollRef.current.scrollTop = logScrollRef.current.scrollHeight;
    }
  }, [logsText]);

  async function handleRestart() {
    if (!window.confirm(t('system:comfyui.restartConfirm'))) return;
    setRestartLoading(true);
    try {
      type RestartResp = { status: string; message: string; updatedAt: string };
      const res = await post<RestartResp>('/api/system/comfyui/restart', {});
      show(t('system:comfyui.restartSuccess') + ': ' + res.message, 'ok');
      void fetchComfy();
    } catch (e) {
      show(t('system:comfyui.restartError') + ': ' + (e instanceof ApiError ? e.message : String(e)), 'err');
    } finally {
      setRestartLoading(false);
    }
  }

  const gpus = gpuData?.gpus ?? [];

  return (
    <div className="space-y-6 p-6">
      {/* Header */}
      <header className="flex items-center gap-3">
        <Cpu size={22} className="text-brand" />
        <div>
          <h1 className="text-xl font-semibold">{t('system:title')}</h1>
          <p className="text-sm text-fg-muted">{t('system:subtitle')}</p>
        </div>
      </header>

      {/* GPU Section */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Monitor size={16} className="text-fg-muted" />
            <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
              {t('system:gpu.title')}
            </h2>
            {gpuLoading && <Loader2 size={14} className="animate-spin text-fg-subtle" />}
          </div>
          <button className="btn-ghost text-xs" onClick={fetchGPU} disabled={gpuLoading}>
            <RefreshCw size={13} />
            {t('common:actions.refresh')}
          </button>
        </div>

        {gpuError && (
          <div className="mb-3 rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {gpuError}
          </div>
        )}

        {gpus.length === 0 && !gpuLoading && !gpuError && (
          <div className="card flex flex-col items-center justify-center gap-3 py-12 text-fg-muted">
            <AlertCircle size={28} />
            <div className="text-center">
              <p className="text-sm font-medium">{t('system:gpu.empty')}</p>
              <p className="mt-1 text-xs text-fg-subtle">{t('system:gpu.emptyDetail')}</p>
            </div>
          </div>
        )}

        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {gpus.map((gpu) => (
            <div key={gpu.uuid} className="card space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Monitor size={16} className="text-brand" />
                  <span className="text-sm font-medium">{gpu.name}</span>
                </div>
                <span className="badge">{gpu.index > 0 ? `#${gpu.index}` : 'GPU'}</span>
              </div>
              <div className="space-y-3">
                <MetricBar
                  label={t('system:gpu.utilization')}
                  value={gpu.utilizationGpu}
                  suffix="%"
                  tone={percentColor(gpu.utilizationGpu)}
                  icon={<Activity size={14} />}
                />
                <MetricBar
                  label={t('system:gpu.memory')}
                  value={Math.round((gpu.memoryUsedMiB / Math.max(gpu.memoryTotalMiB, 1)) * 100)}
                  suffix="%"
                  tone={percentColor(Math.round((gpu.memoryUsedMiB / Math.max(gpu.memoryTotalMiB, 1)) * 100))}
                  meta={`${fmtMiB(gpu.memoryUsedMiB)} / ${fmtMiB(gpu.memoryTotalMiB)}`}
                  icon={<HardDrive size={14} />}
                />
                <MetricBar
                  label={t('system:gpu.temperature')}
                  value={gpu.temperatureC}
                  suffix="°C"
                  tone={temperatureColor(gpu.temperatureC)}
                  max={100}
                  icon={<Thermometer size={14} />}
                />
                <MetricBar
                  label={t('system:gpu.power')}
                  value={Math.round((gpu.powerDrawW / Math.max(gpu.powerLimitW, 1)) * 100)}
                  suffix="%"
                  meta={`${gpu.powerDrawW} W / ${gpu.powerLimitW} W`}
                  tone={percentColor(Math.round((gpu.powerDrawW / Math.max(gpu.powerLimitW, 1)) * 100))}
                  icon={<Bolt size={14} />}
                />
              </div>
              <div className="flex flex-wrap gap-x-4 gap-y-1 border-t border-border pt-2 text-xs text-fg-subtle">
                {gpu.driverVersion && (
                  <span>
                    {t('system:gpu.driver')}: {gpu.driverVersion}
                  </span>
                )}
                {gpu.cudaVersion && (
                  <span>
                    {t('system:gpu.cuda')}: {gpu.cudaVersion}
                  </span>
                )}
                <span>UUID: {gpu.uuid.slice(0, 8)}…</span>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* ComfyUI Section */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <TerminalSquare size={16} className="text-fg-muted" />
            <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
              {t('system:comfyui.title')}
            </h2>
            {comfyLoading && <Loader2 size={14} className="animate-spin text-fg-subtle" />}
          </div>
          <div className="flex items-center gap-2">
            <button className="btn-ghost text-xs" onClick={fetchComfy} disabled={comfyLoading}>
              <RefreshCw size={13} />
              {t('common:actions.refresh')}
            </button>
            <button
              className={`btn text-xs ${restartLoading ? 'btn-ghost' : 'btn-primary'}`}
              onClick={handleRestart}
              disabled={restartLoading}
            >
              {restartLoading ? <Loader2 size={13} className="animate-spin" /> : <RotateCcw size={13} />}
              {t('system:comfyui.restart')}
            </button>
          </div>
        </div>

        {!comfyData || (!comfyData.running && (!comfyData.pids || comfyData.pids.length === 0)) ? (
          <div className="card flex flex-col items-center gap-3 py-12 text-fg-muted">
            <PowerOff size={28} />
            <div className="text-center">
              <p className="text-sm font-medium">{t('system:comfyui.notRunning')}</p>
              <p className="mt-1 text-xs text-fg-subtle">{t('system:comfyui.notRunningDetail')}</p>
            </div>
          </div>
        ) : (
          <div className="card space-y-4">
            <div className="flex flex-wrap items-center gap-4">
              <StatusBadge active={comfyData.running} runningLabel={t('system:comfyui.running')} stoppedLabel={t('system:comfyui.stopped')} />
              {comfyData.pids && comfyData.pids.length > 0 && (
                <div className="text-sm text-fg-muted">
                  <span className="text-fg-subtle">{t('system:comfyui.pids')}:</span>{' '}
                  <span className="font-mono text-fg">{comfyData.pids.join(', ')}</span>
                </div>
              )}
              {comfyData.port && (
                <div className="text-sm text-fg-muted">
                  <span className="text-fg-subtle">{t('system:comfyui.port')}:</span>{' '}
                  <span className="font-mono text-fg">{comfyData.port}</span>
                </div>
              )}
              {comfyData.root && (
                <div className="text-sm text-fg-muted">
                  <span className="text-fg-subtle">{t('system:comfyui.root')}:</span>{' '}
                  <span className="font-mono text-xs text-fg">{comfyData.root}</span>
                </div>
              )}
            </div>
            {comfyData.updatedAt && (
              <p className="text-xs text-fg-subtle">
                Updated: {new Date(comfyData.updatedAt).toLocaleString()}
              </p>
            )}
          </div>
        )}
      </section>

      {/* Logs Section */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileText size={16} className="text-fg-muted" />
            <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
              {t('system:logs.title')}
            </h2>
            <span
              className={`inline-flex h-2 w-2 rounded-full transition-colors ${streamConnected ? 'bg-ok' : 'bg-fg-subtle'}`}
              title={streamConnected ? t('system:logs.streamingOn') : t('system:logs.streamingOff')}
            />
          </div>
          <div className="flex items-center gap-2">
            <button className="btn-ghost text-xs" onClick={fetchLogs} disabled={logsLoading || autoStream}>
              <RefreshCw size={13} />
              {t('system:logs.refresh')}
            </button>
            <button
              className={`btn text-xs ${autoStream ? 'btn-primary' : 'btn-ghost'}`}
              onClick={() => setAutoStream((s) => !s)}
            >
              {autoStream ? <Radio size={13} /> : <Pause size={13} />}
              {t('system:logs.autoStream')}
            </button>
          </div>
        </div>

        {logsError && (
          <div className="mb-3 rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {logsError}
          </div>
        )}

        {!logsText && !logsLoading && !logsError && (
          <div className="card flex flex-col items-center gap-3 py-12 text-fg-muted">
            <FileText size={28} />
            <div className="text-center">
              <p className="text-sm font-medium">{t('system:logs.noLogs')}</p>
              <p className="mt-1 text-xs text-fg-subtle">{t('system:logs.noLogsDetail')}</p>
            </div>
          </div>
        )}

        <div className="relative">
          <div
            ref={logScrollRef}
            className="max-h-[32rem] overflow-auto rounded-xl border border-border bg-bg-soft p-3 font-mono text-xs leading-relaxed text-fg-muted"
            onScroll={(e) => {
              const el = e.currentTarget;
              const nearBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + 40;
              shouldScrollRef.current = nearBottom;
            }}
          >
            {logsLoading && (
              <div className="flex items-center gap-2 py-2 text-fg-subtle">
                <Loader2 size={13} className="animate-spin" />
                {t('system:logs.loading')}
              </div>
            )}
            {logsText ? (
              <pre className="whitespace-pre-wrap break-all">
                {logsText.split('\n').map((line, i) => (
                  <span key={i} className="block">
                    {line || ' '}
                  </span>
                ))}
              </pre>
            ) : null}
          </div>
          {/* Bottom fade for visual polish */}
          <div className="pointer-events-none absolute inset-x-0 bottom-0 h-8 rounded-b-xl bg-gradient-to-t from-bg-soft to-transparent" />
        </div>
      </section>

      {/* Toasts */}
      <div className="pointer-events-none fixed right-4 top-4 z-50 flex flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`pointer-events-auto flex items-center gap-2 rounded-lg border px-3 py-2 text-xs shadow-soft transition-all ${
              toast.tone === 'err' ? 'border-err/30 bg-err/10 text-err' : 'border-ok/30 bg-ok/10 text-ok'
            }`}
          >
            <span>{toast.message}</span>
            <button className="ml-auto opacity-70 hover:opacity-100" onClick={() => remove(toast.id)}>
              ×
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

/* =========================== Sub-components ============================= */

function MetricBar({
  label,
  value,
  suffix,
  tone,
  meta,
  max = 100,
  icon,
}: {
  label: string;
  value: number;
  suffix: string;
  tone: 'ok' | 'warn' | 'err';
  meta?: string;
  max?: number;
  icon?: React.ReactNode;
}) {
  const clamped = Math.min(Math.max(value, 0), max);
  const pct = Math.round((clamped / max) * 100);
  const barColor =
    tone === 'ok' ? 'bg-ok' : tone === 'warn' ? 'bg-warn' : 'bg-err';
  const textColor =
    tone === 'ok' ? 'text-ok' : tone === 'warn' ? 'text-warn' : 'text-err';
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="flex items-center gap-1 text-fg-subtle">
          {icon}
          {label}
        </span>
        <span className={`font-medium ${textColor}`}>
          {value}
          {suffix}
          {meta && <span className="ml-1 text-fg-subtle">{meta}</span>}
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-bg">
        <div
          className={`h-full rounded-full transition-all duration-500 ${barColor}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

function StatusBadge({
  active,
  runningLabel,
  stoppedLabel,
}: {
  active: boolean;
  runningLabel: string;
  stoppedLabel: string;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium ${
        active
          ? 'border-ok/30 bg-ok/10 text-ok'
          : 'border-err/30 bg-err/10 text-err'
      }`}
    >
      {active ? <Power size={12} /> : <PowerOff size={12} />}
      {active ? runningLabel : stoppedLabel}
    </span>
  );
}
