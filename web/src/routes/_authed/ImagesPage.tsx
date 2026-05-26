import { useState, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Image as ImageIcon,
  Search,
  RefreshCw,
  ScanSearch,
  Heart,
  Trash2,
  X,
  Copy,
  Check,
  AlertTriangle,
  Loader2,
  Calendar,
  Download,
  Zap,
} from 'lucide-react';
import { get, post, del, ApiError } from '@/lib/api';
import { useToasts } from '@/hooks/use-toasts';

type ImageItem = {
  id: string;
  filename: string;
  relPath: string;
  sizeBytes: number;
  width: number;
  height: number;
  prompt?: string;
  negativePrompt?: string;
  thumbnailPath?: string;
  favorited: boolean;
  tags?: string[];
  createdAt?: string;
};

type ImagesApi = { images: ImageItem[] };
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

function imgUrl(item: ImageItem): string {
  if (item.thumbnailPath) {
    return `/api/files/download?path=${encodeURIComponent(item.thumbnailPath)}`;
  }
  return `/api/files/download?path=${encodeURIComponent(item.relPath)}`;
}

export default function ImagesPage() {
  const { t } = useTranslation(['images','common']);
  const qc = useQueryClient();
  const { toasts, show, remove } = useToasts();

  const [search, setSearch] = useState('');
  const [favoritedOnly, setFavoritedOnly] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [detail, setDetail] = useState<ImageItem | null>(null);
  const [detailTab, setDetailTab] = useState<'info' | 'workflow'>('info');
  const [workflowJson, setWorkflowJson] = useState<string | null>(null);
  const [wfLoading, setWfLoading] = useState(false);
  const [batch, setBatch] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [deleteBusy, setDeleteBusy] = useState<string | null>(null);
  const [copiedPrompt, setCopiedPrompt] = useState(false);

  const activeQ = useQuery({
    queryKey: ['active-instance'],
    queryFn: () => get<{ activeId: number }>('/api/instances/active'),
    refetchInterval: 30_000,
  });

  const hasActive = (activeQ.data?.activeId ?? 0) > 0;

  const imagesQ = useQuery<ImagesApi>({
    queryKey: ['images', search, favoritedOnly],
    queryFn: () => {
      const q = new URLSearchParams();
      if (search) q.set('q', search);
      if (favoritedOnly) q.set('favorited', 'true');
      return get<ImagesApi>(`/api/images?${q.toString()}`);
    },
    enabled: hasActive,
    retry: 1,
  });

  const images = imagesQ.data?.images ?? [];

  // Group by date-ish (yyyy-mm-dd) for masonry sections
  const grouped = useMemo(() => {
    const map = new Map<string, ImageItem[]>();
    for (const img of images) {
      const d = img.createdAt ? img.createdAt.slice(0, 10) : 'Unknown';
      if (!map.has(d)) map.set(d, []);
      map.get(d)!.push(img);
    }
    const arr = Array.from(map.entries());
    arr.sort((a, b) => (a[0] === 'Unknown' ? 1 : b[0] === 'Unknown' ? -1 : b[0].localeCompare(a[0])));
    return arr;
  }, [images]);

  async function doScan() {
    setScanning(true);
    try {
      const res = await post<ScanJob>('/api/images/scan');
      show(`${t('images:scan')} → ${res.status}`);
      void qc.invalidateQueries({ queryKey: ['images'] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('images:scan')} failed: ${msg}`, 'err');
    } finally {
      setScanning(false);
    }
  }

  async function toggleFavorite(id: string, favorited: boolean) {
    try {
      await post(`/api/images/${id}/favorite`, { favorited });
      void qc.invalidateQueries({ queryKey: ['images'] });
      show(favorited ? t('images:detail.favorite') : t('images:detail.unfavorite'));
      if (detail && detail.id === id) {
        setDetail({ ...detail, favorited });
      }
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(msg, 'err');
    }
  }

  async function doDeleteSingle(id: string) {
    if (!window.confirm(t('images:detail.deleteConfirm'))) return;
    setDeleteBusy(id);
    try {
      await del(`/api/images/${id}`);
      show(t('common:actions.delete'));
      setDetail((d) => (d?.id === id ? null : d));
      setSelectedIds((prev) => { const n = new Set(prev); n.delete(id); return n; });
      void qc.invalidateQueries({ queryKey: ['images'] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(msg, 'err');
    } finally {
      setDeleteBusy(null);
    }
  }

  async function openDetail(item: ImageItem) {
    setDetail(item);
    setDetailTab('info');
    setWorkflowJson(null);
    if (item.favorited !== undefined) {
      // no-op; just open
    }
  }

  async function loadWorkflow(id: string) {
    setWfLoading(true);
    try {
      const res = await get<{ workflowJson: unknown }>(`/api/images/${id}/workflow`);
      setWorkflowJson(JSON.stringify(res.workflowJson ?? null, null, 2));
    } catch (e) {
      setWorkflowJson('null');
    } finally {
      setWfLoading(false);
    }
  }

  async function batchZip() {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;
    try {
      const res = await post<Blob>('/api/images/batch-zip', { ids });
      const url = URL.createObjectURL(res);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'images.zip';
      a.click();
      URL.revokeObjectURL(url);
      show(t('images:batch.zip'));
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(msg, 'err');
    }
  }

  async function batchDelete() {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;
    if (!window.confirm(t('images:batch.deleteConfirm', { count: ids.length }))) return;
    for (const id of ids) {
      try {
        await del(`/api/images/${id}`);
      } catch { /* ignore */ }
    }
    setSelectedIds(new Set());
    setBatch(false);
    void qc.invalidateQueries({ queryKey: ['images'] });
    show(t('images:batch.delete'));
  }

  const toggleSelect = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const n = new Set(prev);
      if (n.has(id)) n.delete(id); else n.add(id);
      return n;
    });
  }, []);

  const selectAll = useCallback(() => {
    setSelectedIds(new Set(images.map((i) => i.id)));
  }, [images]);

  if (!hasActive) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="card max-w-md text-center">
          <AlertTriangle className="mx-auto mb-3 text-warn" />
          <h2 className="mb-2 text-lg font-medium">{t('images:title')}</h2>
          <p className="mb-4 text-sm text-fg-muted">{t('images:noActive')}</p>
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
            <ImageIcon size={18} className="text-brand" />
            <h1 className="text-base font-semibold">{t('images:title')}</h1>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <button className="btn-primary text-xs" onClick={doScan} disabled={scanning}>
              {scanning ? <Loader2 size={14} className="animate-spin" /> : <ScanSearch size={14} />}
              {t(scanning ? 'images:scanning' : 'images:scan')}
            </button>
            <button className="btn-ghost text-xs" onClick={() => imagesQ.refetch()} disabled={imagesQ.isFetching}>
              <RefreshCw size={14} className={imagesQ.isFetching ? 'animate-spin' : ''} />
              {t('common:actions.refresh')}
            </button>
            <button
              className={`btn-ghost text-xs ${batch ? 'bg-brand/10 text-brand' : ''}`}
              onClick={() => { setBatch((b) => !b); if (batch) setSelectedIds(new Set()); }}
            >
              <Zap size={14} />
              {batch ? t('images:batch.cancel') : t('images:batch.select')}
            </button>
          </div>
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-muted" />
            <input
              className="input w-56 pl-8 text-xs"
              placeholder={t('images:searchPlaceholder')}
              value={search}
              onChange={(e) => { setSearch(e.target.value); setSelectedIds(new Set()); }}
            />
          </div>
          <div className="flex items-center gap-2 rounded-lg border border-border bg-bg px-2 py-1.5 text-xs">
            <Heart size={12} className={favoritedOnly ? 'text-err' : 'text-fg-muted'} />
            <button
              className={`transition-colors ${favoritedOnly ? 'text-err' : 'text-fg-muted hover:text-fg'}`}
              onClick={() => { setFavoritedOnly((v) => !v); setSelectedIds(new Set()); }}
            >
              {favoritedOnly ? t('images:favorites') : t('images:all')}
            </button>
          </div>

          {batch && selectedIds.size > 0 && (
            <div className="ml-auto flex items-center gap-2 rounded-lg border border-brand/20 bg-brand/10 px-3 py-1.5 text-xs">
              <span className="text-brand">{t('images:batch.selected', { count: selectedIds.size })}</span>
              <button className="text-brand hover:underline" onClick={selectAll}>{t('images:batch.select')}</button>
              <button className="text-fg-muted hover:text-fg" onClick={() => setSelectedIds(new Set())}>{t('common:actions.cancel')}</button>
              <button className="ml-2 flex items-center gap-1 text-brand hover:underline" onClick={batchZip}>
                <Download size={12} />
                {t('images:batch.zip')}
              </button>
              <button className="flex items-center gap-1 text-err hover:underline" onClick={batchDelete}>
                <Trash2 size={12} />
                {t('images:batch.delete')}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto p-5">
        {imagesQ.isLoading && (
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <Loader2 size={16} className="animate-spin" />
            {t('common:actions.loading')}
          </div>
        )}
        {imagesQ.isError && (
          <div className="rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {imagesQ.error instanceof Error ? imagesQ.error.message : String(imagesQ.error)}
          </div>
        )}
        {!imagesQ.isLoading && images.length === 0 && (
          <div className="text-sm text-fg-muted">{t('images:empty')}</div>
        )}

        {/* Masonry-ish grouped grid */}
        <div className="space-y-6">
          {grouped.map(([date, list]) => (
            <div key={date}>
              <div className="mb-2 flex items-center gap-2 text-xs font-medium text-fg-subtle">
                <Calendar size={12} />
                {date}
                <span className="text-fg-muted">({list.length})</span>
              </div>
              <div className="columns-1 gap-3 sm:columns-2 md:columns-3 lg:columns-4 xl:columns-5">
                {list.map((img) => (
                  <div
                    key={img.id}
                    className={`group relative mb-3 break-inside-avoid rounded-xl border transition-all hover:shadow-soft ${
                      selectedIds.has(img.id) ? 'border-brand/50 bg-brand/5' : 'border-border hover:border-brand/30'
                    }`}
                  >
                    <div className="relative overflow-hidden rounded-t-xl">
                      <img
                        src={imgUrl(img)}
                        alt={img.filename}
                        className="w-full cursor-zoom-in object-cover"
                        loading="lazy"
                        onClick={() => { if (!batch) openDetail(img); }}
                      />
                      {/* Favorite marker */}
                      {img.favorited && (
                        <div className="absolute left-2 top-2">
                          <Heart size={14} className="fill-err text-err" />
                        </div>
                      )}
                      {/* Batch checkbox */}
                      {batch && (
                        <button
                          className="absolute right-2 top-2 rounded-full bg-black/40 p-1.5 text-white"
                          onClick={(e) => { e.stopPropagation(); toggleSelect(img.id); }}
                          title={t('images:batch.select')}
                        >
                          {selectedIds.has(img.id) ? <Check size={12} /> : <div className="h-3 w-3 rounded-sm border-2 border-white/80" />}
                        </button>
                      )}
                      {/* Hover actions */}
                      {!batch && (
                        <div className="absolute inset-x-0 bottom-0 flex items-center justify-between bg-gradient-to-t from-black/70 to-transparent p-2 opacity-0 transition-opacity group-hover:opacity-100">
                          <button
                            className="rounded-full bg-white/10 p-1.5 text-white backdrop-blur hover:bg-white/20"
                            title={img.favorited ? t('images:detail.unfavorite') : t('images:detail.favorite')}
                            onClick={(e) => { e.stopPropagation(); toggleFavorite(img.id, !img.favorited); }}
                          >
                            <Heart size={14} className={img.favorited ? 'fill-err text-err' : ''} />
                          </button>
                          <div className="flex gap-1">
                            <button
                              className="rounded-full bg-white/10 p-1.5 text-white backdrop-blur hover:bg-white/20"
                              title={t('images:detail.delete')}
                              onClick={(e) => { e.stopPropagation(); doDeleteSingle(img.id); }}
                              disabled={deleteBusy === img.id}
                            >
                              {deleteBusy === img.id ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                    {/* Caption */}
                    <div className="px-3 py-2">
                      <div className="truncate text-xs text-fg-muted" title={img.filename}>{img.filename}</div>
                      <div className="mt-0.5 flex items-center gap-2 text-[11px] text-fg-subtle">
                        <span>{img.width}×{img.height}</span>
                        <span>{fmtSize(img.sizeBytes)}</span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Detail modal */}
      {detail && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" onClick={() => setDetail(null)}>
          <div
            className="card flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold">{t('images:detail.title')}</h3>
              <button className="btn-ghost p-1" onClick={() => setDetail(null)}><X size={16} /></button>
            </div>

            {/* Body */}
            <div className="flex flex-1 overflow-hidden flex-col md:flex-row">
              {/* Preview */}
              <div className="flex items-center justify-center bg-bg-soft p-4 md:w-1/2">
                <img
                  src={imgUrl(detail)}
                  alt={detail.filename}
                  className="max-h-[60vh] rounded-lg object-contain shadow-soft"
                  onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }}
                />
              </div>

              {/* Sidebar info */}
              <div className="flex flex-1 flex-col border-l border-border md:w-1/2">
                {/* Tabs */}
                <div className="flex border-b border-border">
                  <button
                    className={`flex-1 px-4 py-2 text-xs font-medium transition-colors ${detailTab === 'info' ? 'border-b-2 border-brand text-brand' : 'text-fg-muted hover:text-fg'}`}
                    onClick={() => { setDetailTab('info'); }}
                  >
                    {t('images:detail.preview')}
                  </button>
                  <button
                    className={`flex-1 px-4 py-2 text-xs font-medium transition-colors ${detailTab === 'workflow' ? 'border-b-2 border-brand text-brand' : 'text-fg-muted hover:text-fg'}`}
                    onClick={() => { setDetailTab('workflow'); if (!workflowJson) loadWorkflow(detail.id); }}
                  >
                    {t('images:detail.workflow')}
                  </button>
                </div>

                <div className="flex-1 overflow-auto p-4 space-y-4">
                  {detailTab === 'info' ? (
                    <>
                      {detail.prompt && (
                        <div>
                          <div className="label">{t('images:detail.prompt')}</div>
                          <div className="relative">
                            <div className="max-h-40 overflow-auto break-words rounded-lg border border-border bg-bg-soft px-3 py-2 text-xs leading-relaxed text-fg-muted">
                              {detail.prompt}
                            </div>
                            <button
                              className="absolute right-2 top-2 rounded bg-bg-card p-1 text-fg-muted hover:text-fg shadow-soft"
                              title={t('images:detail.copyPrompt')}
                              onClick={() => {
                                navigator.clipboard.writeText(detail.prompt ?? '');
                                setCopiedPrompt(true);
                                setTimeout(() => setCopiedPrompt(false), 1200);
                              }}
                            >
                              {copiedPrompt ? <Check size={12} className="text-ok" /> : <Copy size={12} />}
                            </button>
                          </div>
                        </div>
                      )}

                      {detail.negativePrompt && (
                        <div>
                          <div className="label">{t('images:detail.negativePrompt')}</div>
                          <div className="max-h-32 overflow-auto break-words rounded-lg border border-border bg-bg-soft px-3 py-2 text-xs leading-relaxed text-fg-muted">
                            {detail.negativePrompt}
                          </div>
                        </div>
                      )}

                      <div className="grid grid-cols-2 gap-3">
                        <div>
                          <div className="label">{t('images:detail.dimensions')}</div>
                          <div className="text-sm text-fg">{detail.width}×{detail.height}</div>
                        </div>
                        <div>
                          <div className="label">{t('images:detail.size')}</div>
                          <div className="text-sm text-fg">{fmtSize(detail.sizeBytes)}</div>
                        </div>
                        <div>
                          <div className="label">{t('images:detail.createdAt')}</div>
                          <div className="text-xs text-fg-muted">{fmtDate(detail.createdAt)}</div>
                        </div>
                        <div>
                          <div className="label">{t('images:detail.path')}</div>
                          <div className="break-all text-xs text-fg-muted">{detail.relPath}</div>
                        </div>
                      </div>

                      {detail.tags && detail.tags.length > 0 && (
                        <div>
                          <div className="label">Tags</div>
                          <div className="flex flex-wrap gap-1.5">
                            {detail.tags.map((tag) => (
                              <span key={tag} className="badge">{tag}</span>
                            ))}
                          </div>
                        </div>
                      )}
                    </>
                  ) : (
                    <div>
                      {wfLoading ? (
                        <div className="flex items-center gap-2 text-xs text-fg-muted">
                          <Loader2 size={14} className="animate-spin" />
                          {t('common:actions.loading')}
                        </div>
                      ) : (
                        <pre className="max-h-[50vh] overflow-auto rounded-lg border border-border bg-bg-soft p-3 text-xs font-mono text-fg-muted">
                          {workflowJson ?? 'No workflow data'}
                        </pre>
                      )}
                    </div>
                  )}
                </div>

                {/* Footer actions */}
                <div className="flex items-center gap-2 border-t border-border px-4 py-3">
                  <button
                    className="btn-primary text-xs"
                    onClick={() => toggleFavorite(detail.id, !detail.favorited)}
                  >
                    <Heart size={14} className={detail.favorited ? 'fill-err text-err' : ''} />
                    {detail.favorited ? t('images:detail.unfavorite') : t('images:detail.favorite')}
                  </button>
                  <a
                    className="btn-ghost text-xs"
                    href={imgUrl(detail)}
                    download={detail.filename}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <Download size={14} />
                    {t('common:actions.open')}
                  </a>
                  <button
                    className="btn-danger ml-auto text-xs"
                    onClick={() => doDeleteSingle(detail.id)}
                    disabled={deleteBusy === detail.id}
                  >
                    {deleteBusy === detail.id ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    {t('images:detail.delete')}
                  </button>
                </div>
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
