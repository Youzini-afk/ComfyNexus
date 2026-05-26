import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Download, Loader2, Pause, Play, RefreshCw, XCircle } from 'lucide-react';
import { get, del } from '@/lib/api';

type DownloadJob = {
  id: string;
  url: string;
  destPath: string;
  status: 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled';
  progress?: number;
  size?: number;
  speed?: number;
  error?: string;
  createdAt?: string;
  updatedAt?: string;
};

type JobsApi = { jobs: DownloadJob[] };

function fmtSize(n: number | undefined): string {
  if (n == null || n < 0) return '-';
  if (n === 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let s = 0;
  let val = n;
  while (Math.abs(val) >= 1024 && s < u.length - 1) {
    val /= 1024;
    s++;
  }
  return `${Math.floor(val * 10) / 10} ${u[s]}`;
}

function fmtSpeed(n: number | undefined): string {
  if (!n || n <= 0) return '-';
  return `${fmtSize(n)}/s`;
}

function fmtDate(s: string | undefined): string {
  if (!s) return '';
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  try {
    return d.toLocaleString(undefined);
  } catch {
    return s;
  }
}

export default function DownloadsPage() {
  const { t } = useTranslation(['downloads', 'common']);
  const qc = useQueryClient();

  const jobs = useQuery<JobsApi>({
    queryKey: ['downloads'],
    queryFn: () => get<JobsApi>('/api/downloads'),
    retry: 1,
    refetchInterval: 3000,
    refetchOnWindowFocus: true,
  });

  const sorted = useMemo(() => {
    const arr = jobs.data?.jobs ?? [];
    const order = ['running', 'pending', 'paused', 'failed', 'completed', 'cancelled'];
    return [...arr].sort((a, b) => {
      const oa = order.indexOf(a.status);
      const ob = order.indexOf(b.status);
      if (oa !== ob) return oa - ob;
      return (b.updatedAt ?? '').localeCompare(a.updatedAt ?? '');
    });
  }, [jobs.data]);

  const cancel = useMutation({
    mutationFn: async (id: string) => {
      await del(`/api/downloads/${id}`);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['downloads'] });
    },
  });

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 border-b border-border bg-bg-soft px-5 py-3">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="flex items-center gap-2 text-lg font-semibold">
              <Download size={18} className="text-brand" />
              {t('downloads:title')}
            </h1>
            <p className="text-xs text-fg-muted">{t('downloads:subtitle')}</p>
          </div>
          <button className="btn-ghost text-xs" onClick={() => jobs.refetch()}>
            <RefreshCw size={14} />
            {t('downloads:actions.refresh')}
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-auto p-5">
        {jobs.isLoading && (
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <Loader2 size={16} className="animate-spin" />
            {t('common:actions.loading')}
          </div>
        )}

        {jobs.isError && (
          <div className="rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {jobs.error instanceof Error ? jobs.error.message : String(jobs.error)}
          </div>
        )}

        {!jobs.isLoading && sorted.length === 0 && (
          <div className="rounded-lg border border-border bg-bg-soft p-6 text-center text-sm text-fg-muted">
            {t('downloads:empty')}
          </div>
        )}

        <div className="space-y-2">
          {sorted.map((j) => (
            <div key={j.id} className="card flex flex-col gap-2 p-3">
              <div className="flex items-start gap-3">
                <div className="mt-0.5 shrink-0 text-fg-muted">
                  {statusIcon(j.status)}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{filename(j.destPath) || j.url}</div>
                  <div className="mt-0.5 truncate text-xs text-fg-muted">{j.url}</div>
                  <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-fg-subtle">
                    <span>{t('downloads:columns.destPath')}: {j.destPath}</span>
                    <span>{t('downloads:columns.size')}: {fmtSize(j.size)}</span>
                    <span>{t('downloads:columns.speed')}: {fmtSpeed(j.speed)}</span>
                    <span>{fmtDate(j.updatedAt)}</span>
                  </div>
                </div>
                <div className="flex items-center gap-1">
                  {(j.status === 'running' || j.status === 'pending' || j.status === 'paused') && (
                    <button
                      className="btn-ghost p-1 text-xs"
                      title={t('downloads:actions.cancel')}
                      onClick={() => cancel.mutate(j.id)}
                    >
                      <XCircle size={14} />
                    </button>
                  )}
                </div>
              </div>

              {j.status === 'running' && typeof j.progress === 'number' && j.progress >= 0 && j.progress <= 100 && (
                <div className="mt-1">
                  <div className="flex items-center justify-between text-xs text-fg-muted">
                    <span>{t('downloads:columns.progress')}</span>
                    <span>{Math.round(j.progress)}%</span>
                  </div>
                  <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-bg">
                    <div
                      className="h-full rounded-full bg-brand transition-all"
                      style={{ width: `${j.progress}%` }}
                    />
                  </div>
                </div>
              )}

              {j.error && (
                <div className="rounded-md border border-err/30 bg-err/10 px-3 py-1.5 text-xs text-err">
                  {j.error}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function statusIcon(status: DownloadJob['status']) {
  switch (status) {
    case 'completed':
      return <Download size={16} className="text-ok" />;
    case 'failed':
      return <XCircle size={16} className="text-err" />;
    case 'paused':
      return <Pause size={16} className="text-warn" />;
    case 'running':
      return <Loader2 size={16} className="animate-spin text-brand" />;
    case 'cancelled':
      return <XCircle size={16} className="text-fg-subtle" />;
    default:
      return <Play size={16} className="text-fg-muted" />;
  }
}

function filename(p: string) {
  if (!p) return '';
  const i = p.lastIndexOf('/');
  return i >= 0 ? p.slice(i + 1) : p;
}
