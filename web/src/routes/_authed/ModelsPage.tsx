import { useState, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Boxes,
  Search,
  RefreshCw,
  ScanSearch,
  LayoutGrid,
  List as ListIcon,
  Trash2,
  X,
  ExternalLink,
  Copy,
  Check,
  AlertTriangle,
  HardDrive,
  Layers,
  Loader2,
} from 'lucide-react';
import { get, post, del, ApiError } from '@/lib/api';
import { useToasts } from '@/hooks/use-toasts';

type Model = {
  id: string;
  modelType: string;
  filename: string;
  relPath: string;
  sizeBytes: number;
  sha256?: string;
  civitaiModelId?: string;
  civitaiVersionId?: string;
  triggerWords?: string[];
  baseModel?: string;
  thumbnailPath?: string;
  tags?: string[];
  scannedAt?: string;
  civitaiSyncedAt?: string;
};

type ModelsApi = { models: Model[] };
type DiskUsageApi = { items: { type: string; totalSize: number; count: number }[] };

type ScanJob = { jobId: string; status: string };

function fmtSize(n: number): string {
  if (n === 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let s = 0;
  while (Math.abs(n) >= 1024 && s < u.length - 1) {
    n /= 1024;
    s++;
  }
  return `${Math.floor(n * 10) / 10} ${u[s]}`;
}

function fmtDate(s?: string): string {
  if (!s) return '-';
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  try {
    return d.toLocaleString();
  } catch {
    return s;
  }
}

const MODEL_TYPES = ['checkpoint','lora','embedding','controlnet','upscale','vae'];
const TYPE_ICONS: Record<string, React.ReactNode> = {
  checkpoint: <Layers size={14} />,
  lora: <Layers size={14} />,
  embedding: <Layers size={14} />,
  controlnet: <Layers size={14} />,
  upscale: <Layers size={14} />,
  vae: <Layers size={14} />,
};

export default function ModelsPage() {
  const { t } = useTranslation(['models','common','errors']);
  const qc = useQueryClient();
  const { toasts, show, remove } = useToasts();

  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [view, setView] = useState<'grid' | 'list'>('grid');
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [detail, setDetail] = useState<Model | null>(null);
  const [scanning, setScanning] = useState(false);
  const [syncingId, setSyncingId] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState<string | null>(null);
  const [copiedSha, setCopiedSha] = useState(false);

  const activeQ = useQuery({
    queryKey: ['active-instance'],
    queryFn: () => get<{ activeId: number }>('/api/instances/active'),
    refetchInterval: 30_000,
  });

  const hasActive = (activeQ.data?.activeId ?? 0) > 0;

  const modelsQ = useQuery<ModelsApi>({
    queryKey: ['models', search, typeFilter],
    queryFn: () => {
      const q = new URLSearchParams();
      if (search) q.set('q', search);
      if (typeFilter) q.set('type', typeFilter);
      return get<ModelsApi>(`/api/models?${q.toString()}`);
    },
    enabled: hasActive,
    retry: 1,
  });

  const diskQ = useQuery<DiskUsageApi>({
    queryKey: ['models-disk-usage'],
    queryFn: () => get<DiskUsageApi>('/api/models/disk-usage'),
    enabled: hasActive,
    retry: 1,
  });

  const models = modelsQ.data?.models ?? [];

  const diskTotal = useMemo(() => {
    return (diskQ.data?.items ?? []).reduce((sum, it) => sum + it.totalSize, 0);
  }, [diskQ.data]);

  const diskCount = useMemo(() => {
    return (diskQ.data?.items ?? []).reduce((sum, it) => sum + it.count, 0);
  }, [diskQ.data]);

  async function doScan() {
    setScanning(true);
    try {
      const res = await post<ScanJob>('/api/models/scan');
      show(`${t('models:scan')} → ${res.status}`);
      void qc.invalidateQueries({ queryKey: ['models'] });
      void qc.invalidateQueries({ queryKey: ['models-disk-usage'] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('models:scan')} failed: ${msg}`, 'err');
    } finally {
      setScanning(false);
    }
  }

  async function doSync(id: string) {
    setSyncingId(id);
    try {
      const res = await post<ScanJob>(`/api/models/${id}/sync-civitai`);
      show(`${t('models:syncCivitai')} → ${res.status}`);
      void qc.invalidateQueries({ queryKey: ['models'] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('models:syncCivitai')} failed: ${msg}`, 'err');
    } finally {
      setSyncingId(null);
    }
  }

  async function doDelete(id: string, name: string) {
    if (!window.confirm(t('models:deleteConfirm', { name }))) return;
    setDeleteBusy(id);
    try {
      await del(`/api/models/${id}`);
      show(t('common:actions.delete'));
      setSelectedIds((prev) => { const n = new Set(prev); n.delete(id); return n; });
      setDetail((d) => (d?.id === id ? null : d));
      void qc.invalidateQueries({ queryKey: ['models'] });
      void qc.invalidateQueries({ queryKey: ['models-disk-usage'] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('models:deleteConfirm')} failed: ${msg}`, 'err');
    } finally {
      setDeleteBusy(null);
    }
  }

  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const n = new Set(prev);
      if (n.has(id)) n.delete(id); else n.add(id);
      return n;
    });
  }, []);

  const selectAll = useCallback(() => {
    setSelectedIds(new Set(models.map((m) => m.id)));
  }, [models]);

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set());
  }, []);

  if (!hasActive) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="card max-w-md text-center">
          <AlertTriangle className="mx-auto mb-3 text-warn" />
          <h2 className="mb-2 text-lg font-medium">{t('models:title')}</h2>
          <p className="mb-4 text-sm text-fg-muted">{t('models:noActive')}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header toolbar */}
      <div className="shrink-0 border-b border-border bg-bg-soft px-5 py-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <Boxes size={18} className="text-brand" />
            <h1 className="text-base font-semibold">{t('models:title')}</h1>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <button className="btn-primary text-xs" onClick={doScan} disabled={scanning}>
              {scanning ? <Loader2 size={14} className="animate-spin" /> : <ScanSearch size={14} />}
              {t(scanning ? 'models:scanning' : 'models:scan')}
            </button>
            <button
              className="btn-ghost text-xs"
              onClick={() => modelsQ.refetch()}
              disabled={modelsQ.isFetching}
            >
              <RefreshCw size={14} className={modelsQ.isFetching ? 'animate-spin' : ''} />
              {t('common:actions.refresh')}
            </button>
            <div className="flex items-center rounded-lg border border-border bg-bg px-1 py-1">
              <button
                className={`rounded px-2 py-1 text-xs transition-colors ${view === 'grid' ? 'bg-bg-card text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
                onClick={() => setView('grid')}
                title={t('models:grid')}
              >
                <LayoutGrid size={14} />
              </button>
              <button
                className={`rounded px-2 py-1 text-xs transition-colors ${view === 'list' ? 'bg-bg-card text-fg shadow-sm' : 'text-fg-muted hover:text-fg'}`}
                onClick={() => setView('list')}
                title={t('models:list')}
              >
                <ListIcon size={14} />
              </button>
            </div>
          </div>
        </div>

        {/* Search / filters */}
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-muted" />
            <input
              className="input w-56 pl-8 text-xs"
              placeholder={t('models:searchPlaceholder')}
              value={search}
              onChange={(e) => { setSearch(e.target.value); setSelectedIds(new Set()); }}
            />
          </div>
          <select
            className="input w-40 text-xs py-1.5"
            value={typeFilter}
            onChange={(e) => { setTypeFilter(e.target.value); setSelectedIds(new Set()); }}
          >
            <option value="">{t('models:allTypes')}</option>
            {MODEL_TYPES.map((t2) => (
              <option key={t2} value={t2}>{t(`models:type.${t2}` as any)}</option>
            ))}
          </select>

          {/* Disk usage mini summary */}
          {!diskQ.isLoading && (
            <div className="ml-auto flex items-center gap-3">
              <div className="flex items-center gap-1.5 rounded-lg border border-border bg-bg px-2.5 py-1.5 text-xs">
                <HardDrive size={13} className="text-fg-muted" />
                <span className="text-fg-muted">{t('models:diskUsage')}</span>
                <span className="font-medium text-fg">{fmtSize(diskTotal)}</span>
              </div>
              <div className="flex items-center gap-1.5 rounded-lg border border-border bg-bg px-2.5 py-1.5 text-xs">
                <Boxes size={13} className="text-fg-muted" />
                <span className="text-fg-muted">{t('models:totalModels')}</span>
                <span className="font-medium text-fg">{diskCount}</span>
              </div>
            </div>
          )}
        </div>

        {/* Selection bar */}
        {selectedIds.size > 0 && (
          <div className="mt-2 flex items-center gap-2 rounded-lg border border-brand/20 bg-brand/10 px-3 py-1.5 text-xs">
            <span className="text-brand">{t('models:selected', { count: selectedIds.size })}</span>
            <div className="ml-auto flex items-center gap-2">
              <button className="text-brand hover:underline" onClick={selectAll}>{t('models:select')}</button>
              <button className="text-fg-muted hover:text-fg" onClick={clearSelection}>{t('common:actions.cancel')}</button>
            </div>
          </div>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto p-5">
        {modelsQ.isLoading && (
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <Loader2 size={16} className="animate-spin" />
            {t('common:actions.loading')}
          </div>
        )}
        {modelsQ.isError && (
          <div className="rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {modelsQ.error instanceof Error ? modelsQ.error.message : String(modelsQ.error)}
          </div>
        )}
        {!modelsQ.isLoading && models.length === 0 && (
          <div className="text-sm text-fg-muted">{t('models:empty')}</div>
        )}

        {view === 'grid' ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {models.map((m) => (
              <div
                key={m.id}
                className={`group relative cursor-pointer rounded-xl border transition-all hover:shadow-soft ${selectedIds.has(m.id) ? 'border-brand/50 bg-brand/5' : 'border-border bg-bg-card hover:border-brand/30'}`}
                onClick={() => setDetail(m)}
              >
                <div className="relative aspect-video overflow-hidden rounded-t-xl">
                  {m.thumbnailPath ? (
                    <img
                      src={`/api/files/download?path=${encodeURIComponent(m.thumbnailPath)}`}
                      alt={m.filename}
                      className="h-full w-full object-cover"
                      loading="lazy"
                      onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }}
                    />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center bg-bg-soft">
                      <Boxes size={28} className="text-fg-subtle" />
                    </div>
                  )}
                  <div className="absolute left-2 top-2">
                    <span className="badge">{t(`models:type.${m.modelType}` as any) ?? m.modelType}</span>
                  </div>
                  <button
                    className="absolute right-2 top-2 rounded-full bg-black/40 p-1.5 text-white opacity-0 transition-opacity group-hover:opacity-100"
                    onClick={(e) => { e.stopPropagation(); toggleSelect(m.id); }}
                    title={t('models:select')}
                  >
                    {selectedIds.has(m.id) ? <Check size={12} /> : <div className="h-3 w-3 rounded-sm border-2 border-white/80" />}
                  </button>
                </div>
                <div className="p-3">
                  <div className="truncate text-sm font-medium text-fg" title={m.filename}>{m.filename}</div>
                  <div className="mt-1 flex items-center justify-between text-xs text-fg-muted">
                    <span>{fmtSize(m.sizeBytes)}</span>
                    {m.baseModel && <span className="badge">{m.baseModel}</span>}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {(m.tags ?? []).slice(0, 4).map((tag) => (
                      <span key={tag} className="badge">{tag}</span>
                    ))}
                    {(m.tags ?? []).length > 4 && (
                      <span className="badge">+{(m.tags ?? []).length - 4}</span>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="space-y-1">
            {models.map((m) => (
              <div
                key={m.id}
                className={`group flex items-center gap-3 rounded-lg border px-3 py-2 transition-all ${selectedIds.has(m.id) ? 'border-brand/40 bg-brand/5' : 'border-transparent hover:border-border hover:bg-bg-card'}`}
              >
                <button
                  className="shrink-0 rounded p-1 text-fg-muted hover:bg-bg-soft"
                  onClick={() => toggleSelect(m.id)}
                >
                  {selectedIds.has(m.id) ? <Check size={14} className="text-brand" /> : <div className="h-3.5 w-3.5 rounded-sm border-2 border-fg-muted" />}
                </button>
                <div className="shrink-0 text-fg-subtle">
                  {TYPE_ICONS[m.modelType] ?? <Boxes size={16} />}
                </div>
                <button
                  className="min-w-0 flex-1 truncate text-left text-sm font-medium text-fg hover:underline"
                  onClick={() => setDetail(m)}
                >
                  {m.filename}
                </button>
                <div className="hidden items-center gap-4 text-xs text-fg-muted md:flex">
                  <span className="w-20 text-right">{t(`models:type.${m.modelType}` as any) ?? m.modelType}</span>
                  <span className="w-24 text-right">{fmtSize(m.sizeBytes)}</span>
                  {m.baseModel && <span className="w-28 truncate text-right">{m.baseModel}</span>}
                  <span className="w-32 text-right">{fmtDate(m.scannedAt)}</span>
                </div>
                <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                  <button
                    className="btn-ghost p-1 text-xs"
                    title={t('models:syncCivitai')}
                    onClick={() => doSync(m.id)}
                    disabled={syncingId === m.id}
                  >
                    {syncingId === m.id ? <Loader2 size={14} className="animate-spin text-brand" /> : <ExternalLink size={14} />}
                  </button>
                  <button
                    className="btn-danger p-1 text-xs"
                    title={t('common:actions.delete')}
                    onClick={() => doDelete(m.id, m.filename)}
                    disabled={deleteBusy === m.id}
                  >
                    {deleteBusy === m.id ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Detail drawer */}
      {detail && (
        <div className="fixed inset-0 z-50 flex justify-end bg-black/50" onClick={() => setDetail(null)}>
          <div
            className="h-full w-full max-w-md overflow-auto border-l border-border bg-bg-card shadow-soft"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold">{t('models:detail.title')}</h3>
              <button className="btn-ghost p-1" onClick={() => setDetail(null)}><X size={16} /></button>
            </div>
            <div className="p-4 space-y-4">
              {/* Thumbnail */}
              <div className="relative aspect-video overflow-hidden rounded-lg bg-bg-soft">
                {detail.thumbnailPath ? (
                  <img
                    src={`/api/files/download?path=${encodeURIComponent(detail.thumbnailPath)}`}
                    alt={detail.filename}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <div className="flex h-full w-full items-center justify-center">
                    <Boxes size={32} className="text-fg-subtle" />
                  </div>
                )}
              </div>

              <div>
                <div className="label">{t('models:detail.path')}</div>
                <div className="break-all rounded-lg border border-border bg-bg-soft px-3 py-2 text-xs font-mono text-fg-muted">{detail.relPath}</div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <div className="label">{t('models:detail.size')}</div>
                  <div className="text-sm text-fg">{fmtSize(detail.sizeBytes)}</div>
                </div>
                <div>
                  <div className="label">{t('models:detail.baseModel')}</div>
                  <div className="text-sm text-fg">{detail.baseModel ?? '-'}</div>
                </div>
              </div>

              {detail.sha256 && (
                <div>
                  <div className="label">{t('models:detail.sha256')}</div>
                  <div className="flex items-center gap-2">
                    <div className="flex-1 break-all rounded-lg border border-border bg-bg-soft px-3 py-2 text-xs font-mono text-fg-muted">{detail.sha256}</div>
                    <button
                      className="btn-ghost p-1"
                      onClick={() => {
                        navigator.clipboard.writeText(detail.sha256 ?? '');
                        setCopiedSha(true);
                        setTimeout(() => setCopiedSha(false), 1200);
                      }}
                      title={t('models:detail.copy')}
                    >
                      {copiedSha ? <Check size={14} className="text-ok" /> : <Copy size={14} />}
                    </button>
                  </div>
                </div>
              )}

              {detail.triggerWords && detail.triggerWords.length > 0 && (
                <div>
                  <div className="label">{t('models:detail.triggerWords')}</div>
                  <div className="flex flex-wrap gap-1.5">
                    {detail.triggerWords.map((w) => (
                      <span key={w} className="badge">{w}</span>
                    ))}
                  </div>
                </div>
              )}

              {detail.tags && detail.tags.length > 0 && (
                <div>
                  <div className="label">{t('models:detail.tags')}</div>
                  <div className="flex flex-wrap gap-1.5">
                    {detail.tags.map((tag) => (
                      <span key={tag} className="badge">{tag}</span>
                    ))}
                  </div>
                </div>
              )}

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <div className="label">{t('models:detail.scannedAt')}</div>
                  <div className="text-xs text-fg-muted">{fmtDate(detail.scannedAt)}</div>
                </div>
                <div>
                  <div className="label">{t('models:detail.syncedAt')}</div>
                  <div className="text-xs text-fg-muted">{fmtDate(detail.civitaiSyncedAt)}</div>
                </div>
              </div>

              {detail.civitaiModelId && (
                <div>
                  <div className="label">{t('models:detail.civitaiId')}</div>
                  <div className="text-sm text-fg">{detail.civitaiModelId}</div>
                </div>
              )}

              <div className="flex items-center gap-2 pt-2">
                <button
                  className="btn-primary text-xs"
                  onClick={() => doSync(detail.id)}
                  disabled={syncingId === detail.id}
                >
                  {syncingId === detail.id ? <Loader2 size={14} className="animate-spin" /> : <ExternalLink size={14} />}
                  {t('models:syncCivitai')}
                </button>
                <button
                  className="btn-danger text-xs"
                  onClick={() => doDelete(detail.id, detail.filename)}
                  disabled={deleteBusy === detail.id}
                >
                  {deleteBusy === detail.id ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                  {t('models:detail.delete')}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Toasts */}
      <div className="pointer-events-none fixed right-4 top-4 z-50 flex flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`pointer-events-auto flex items-center gap-2 rounded-lg border px-3 py-2 text-xs shadow-soft transition-all ${toast.tone === 'err' ? 'border-err/30 bg-err/10 text-err' : 'border-ok/30 bg-ok/10 text-ok'}`}
          >
            <span>{toast.message}</span>
            <button className="ml-auto opacity-70 hover:opacity-100" onClick={() => remove(toast.id)}>
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
