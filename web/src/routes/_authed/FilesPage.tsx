import { useMemo, useRef, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  ChevronRight,
  Download,
  File,
  Folder,
  FolderPlus,
  FolderUp,
  Globe,
  HardDriveUpload,
  Loader2,
  Pencil,
  Trash2,
  UploadCloud,
  X,
  RefreshCw,
  ChevronLeft,
} from 'lucide-react';
import { get, post, del, putBinary, ApiError } from '@/lib/api';

const DEFAULT_CHUNK = 50 * 1024 * 1024; // 50 MB

const ROOTS = ['/models', '/input', '/output', '/custom_nodes', '/user'];

function useQueryPath() {
  const loc = useLocation();
  const params = new URLSearchParams(loc.search);
  return params.get('path') ?? '';
}

function buildSearch(path: string) {
  const q = new URLSearchParams();
  if (path) q.set('path', path);
  return `/files${q.toString() ? `?${q.toString()}` : ''}`;
}

function fmtSize(n: number): string {
  if (n === -1) return '-';
  if (n === 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let s = 0;
  while (Math.abs(n) >= 1024 && s < u.length - 1) {
    n /= 1024;
    s++;
  }
  return `${Math.floor(n * 10) / 10} ${u[s]}`;
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

function splitPath(p: string): string[] {
  if (!p) return [];
  return p.split('/').filter(Boolean);
}

function dirOf(p: string): string {
  if (!p) return '';
  const i = p.lastIndexOf('/');
  return i <= 0 ? '' : p.slice(0, i);
}

function joinPath(a: string, b: string) {
  if (!a) return b;
  return `${a.replace(/\/+$/, '')}/${b}`;
}

function filename(p: string) {
  if (!p) return '';
  const i = p.lastIndexOf('/');
  return i >= 0 ? p.slice(i + 1) : p;
}

/* =========================== Toast ============================= */
export type Toast = { id: number; message: string; tone?: 'ok' | 'err' };

let TOAST_ID = 0;

export function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const show = useCallback((message: string, tone: 'ok' | 'err' = 'ok') => {
    const id = ++TOAST_ID;
    setToasts((prev) => [...prev, { id, message, tone }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 2800);
  }, []);
  const remove = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);
  return { toasts, show, remove };
}

/* =========================== FilesPage ============================= */
type DirEntry = { name: string; path: string; type: 'dir' | 'file' | 'symlink'; size: number; modTime?: string };
type FilesApi = { entries: DirEntry[] };
type UploadJob = { file: File; jobId: string; total: number; loaded: number; done: boolean; error: string; aborted: boolean };
type UploadInit = { jobId: string; uploadedSize?: number };

export default function FilesPage() {
  const { t } = useTranslation(['files', 'common']);
  const qc = useQueryClient();
  const nav = useNavigate();
  const path = useQueryPath();
  const { toasts, show, remove } = useToasts();

  const [dropHover, setDropHover] = useState(false);
  const [uploadsOpen, setUploadsOpen] = useState(false);
  const [uploadJobs, setUploadJobs] = useState<UploadJob[]>([]);
  const [activeMenu, setActiveMenu] = useState<'upload' | 'pull' | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [renameTarget, setRenameTarget] = useState<DirEntry | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [mkdirName, setMkdirName] = useState('');
  const [pullUrl, setPullUrl] = useState('');
  const [pullDest, setPullDest] = useState('');

  const list = useQuery<FilesApi>({
    queryKey: ['files', path],
    queryFn: () => get<FilesApi>(`/api/files?path=${encodeURIComponent(path)}`),
    retry: 1,
    refetchOnWindowFocus: true,
  });

  const sorted = useMemo(() => {
    const arr = list.data?.entries ?? [];
    const copy = [...arr];
    copy.sort((a, b) => {
      if (a.type === 'dir' && b.type !== 'dir') return -1;
      if (b.type === 'dir' && a.type !== 'dir') return 1;
      return a.name.localeCompare(b.name);
    });
    return copy;
  }, [list.data]);

  const crumbs = useMemo(() => {
    const parts = splitPath(path);
    return [
      { name: t('files:breadcrumbRoot'), path: '' },
      ...parts.map((_, i) => {
        const p = '/' + parts.slice(0, i + 1).join('/');
        return { name: parts[i], path: p };
      }),
    ];
  }, [path, t]);

  const parentPath = dirOf(path);

  const isRoot = !path || path === '/';

  /* Actions */
  const [renameBusy, setRenameBusy] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState<string | null>(null);

  async function doMkdir() {
    if (!mkdirName.trim()) return;
    const dest = joinPath(path, mkdirName.trim());
    try {
      await post('/api/files/mkdir', { path: dest });
      show(t('files:toast.folderCreated'));
      setMkdirOpen(false);
      setMkdirName('');
      void qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('files:toast.folderFailed')}: ${msg}`, 'err');
    }
  }

  async function doRename(e: React.FormEvent) {
    e.preventDefault();
    if (!renameTarget) return;
    const oldPath = renameTarget.path;
    const newPath = joinPath(dirOf(oldPath), renameValue.trim());
    if (!renameValue.trim() || newPath === oldPath) {
      setRenameTarget(null);
      return;
    }
    setRenameBusy(true);
    try {
      await post('/api/files/rename', { oldPath, newPath });
      show(t('files:toast.renameOk'));
      setRenameTarget(null);
      void qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('files:toast.renameFailed')}: ${msg}`, 'err');
    } finally {
      setRenameBusy(false);
    }
  }

  async function doDelete(entry: DirEntry) {
    const msg =
      entry.type === 'dir'
        ? t('files:dialogs.deleteConfirmFolder', { name: entry.name })
        : t('files:dialogs.deleteConfirm', { name: entry.name });
    if (!window.confirm(msg)) return;
    setDeleteBusy(entry.path);
    try {
      await del(`/api/files?path=${encodeURIComponent(entry.path)}`);
      show(t('files:toast.deleteOk'));
      void qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('files:toast.deleteFailed')}: ${msg}`, 'err');
    } finally {
      setDeleteBusy(null);
    }
  }

  async function doPull() {
    if (!pullUrl.trim()) return;
    const dest = (pullDest.trim() || joinPath(path, filename(pullUrl.trim())));
    try {
      await post('/api/downloads', { url: pullUrl.trim(), destPath: dest });
      show(t('files:toast.pullQueued'));
      setPullUrl('');
      setPullDest('');
      setActiveMenu(null);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      show(`${t('files:toast.pullFailed')}: ${msg}`, 'err');
    }
  }

  /* Upload helpers */
  function enqueueUpload(file: File) {
    const total = file.size;
    const job: UploadJob = {
      file,
      jobId: '',
      total,
      loaded: 0,
      done: false,
      error: '',
      aborted: false,
    };
    setUploadJobs((prev) => [...prev, job]);
    startUpload(job, total);
  }

  async function startUpload(job: UploadJob, total: number) {
    let uploadPath = joinPath(path, job.file.name);
    // Remove leading slash? backend APIs probably expect absolute path. Keep absolute.
    if (!uploadPath.startsWith('/')) uploadPath = '/' + uploadPath;

    let init: UploadInit;
    try {
      init = await post<UploadInit>('/api/uploads', { path: uploadPath, totalSize: total, chunkSize: DEFAULT_CHUNK });
    } catch (e) {
      setUploadJobs((prev) =>
        prev.map((j) =>
          j.file === job.file ? { ...j, error: e instanceof ApiError ? e.message : String(e) } : j
        )
      );
      return;
    }
    const jobId = init.jobId;
    setUploadJobs((prev) => prev.map((j) => (j.file === job.file ? { ...j, jobId } : j)));
    let loaded = init.uploadedSize ?? 0;
    const controller = new AbortController();

    const totalChunks = Math.ceil(total / DEFAULT_CHUNK);
    for (let n = 0; n < totalChunks; n++) {
      const start = n * DEFAULT_CHUNK;
      const end = Math.min(start + DEFAULT_CHUNK, total);
      const chunk = job.file.slice(start, end);
      try {
        await putBinary(`/api/uploads/${jobId}/chunks/${n}`, await chunk.arrayBuffer(), controller.signal);
        loaded = end;
        setUploadJobs((prev) =>
          prev.map((j) =>
            j.file === job.file ? { ...j, loaded } : j
          )
        );
      } catch (e) {
        setUploadJobs((prev) =>
          prev.map((j) =>
            j.file === job.file ? { ...j, error: e instanceof ApiError ? e.message : String(e) } : j
          )
        );
        return;
      }
      // Check if cancelled via state by querying current array could be heavy; keep simple.
    }
    try {
      await post(`/api/uploads/${jobId}/complete`, {});
      setUploadJobs((prev) =>
        prev.map((j) => (j.file === job.file ? { ...j, done: true, loaded: total } : j))
      );
      void qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (e) {
      setUploadJobs((prev) =>
        prev.map((j) =>
          j.file === job.file ? { ...j, error: e instanceof ApiError ? e.message : String(e) } : j
        )
      );
    }
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault();
    setDropHover(false);
    const files: File[] = [];
    if (e.dataTransfer.items) {
      for (let i = 0; i < e.dataTransfer.items.length; i++) {
        const item = e.dataTransfer.items[i];
        if (item.kind === 'file') {
          const f = item.getAsFile();
          if (f) files.push(f);
        }
      }
    } else {
      for (let i = 0; i < (e.dataTransfer.files?.length ?? 0); i++) {
        if (e.dataTransfer.files[i]) files.push(e.dataTransfer.files[i]);
      }
    }
    if (files.length) {
      setUploadsOpen(true);
      files.forEach((f) => enqueueUpload(f));
    }
  }

  function onFileInput(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files;
    if (!files?.length) return;
    setUploadsOpen(true);
    for (let i = 0; i < files.length; i++) enqueueUpload(files[i]);
    // reset input
    if (fileInputRef.current) fileInputRef.current.value = '';
  }

  const activeCount = uploadJobs.filter((j) => !j.done && !j.error && !j.aborted).length;

  return (
    <div
      className={`flex h-full flex-col ${dropHover ? 'bg-brand/5 ring-2 ring-brand/30' : ''}`}
      onDragOver={(e) => {
        e.preventDefault();
        setDropHover(true);
      }}
      onDragLeave={() => setDropHover(false)}
      onDrop={handleDrop}
    >
      {/* Top bar: breadcrumb + actions */}
      <div className="shrink-0 border-b border-border bg-bg-soft px-5 py-3">
        <div className="mb-2 flex flex-wrap items-center gap-2 text-sm">
          <nav className="flex items-center gap-1 overflow-x-auto text-fg-muted">
            {crumbs.map((cr, i) => (
              <span key={cr.path + i} className="flex items-center">
                <button
                  className={`rounded px-1 py-0.5 transition-colors ${i === crumbs.length - 1 ? 'font-medium text-fg' : 'hover:text-fg hover:underline'}`}
                  onClick={() => nav(buildSearch(cr.path))}
                >
                  {cr.name}
                </button>
                {i < crumbs.length - 1 && <ChevronRight size={14} className="mx-0.5 text-fg-subtle" />}
              </span>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-2">
            <button className="btn-ghost text-xs" onClick={() => list.refetch()}>
              <RefreshCw size={14} />
              {t('common:actions.refresh')}
            </button>
            <button className="btn-primary text-xs" onClick={() => setMkdirOpen(true)}>
              <FolderPlus size={14} />
              {t('files:actions.newFolder')}
            </button>
            <button
              className="btn-primary text-xs"
              onClick={() => {
                setActiveMenu('upload');
                fileInputRef.current?.click();
              }}
            >
              <UploadCloud size={14} />
              {t('files:actions.upload')}
            </button>
            <button
              className="btn-ghost text-xs"
              onClick={() => setActiveMenu('pull')}
              title={t('files:actions.remotePull')}
            >
              <Globe size={14} />
            </button>
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="sr-only"
              onChange={onFileInput}
            />
          </div>
        </div>

        {/* Quick roots */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-fg-subtle">{t('files:quickRoots')}</span>
          {ROOTS.map((r) => (
            <button
              key={r}
              className="rounded-md border border-border bg-bg px-2 py-1 text-xs text-fg-muted transition-colors hover:border-brand/40 hover:text-fg"
              onClick={() => nav(buildSearch(r))}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      {/* File list */}
      <div className="flex-1 overflow-auto p-5">
        {list.isLoading && (
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <Loader2 size={16} className="animate-spin" />
            {t('common:actions.loading')}
          </div>
        )}

        {list.isError && (
          <div className="rounded-lg border border-err/30 bg-err/10 px-4 py-3 text-sm text-err">
            {list.error instanceof Error ? list.error.message : String(list.error)}
          </div>
        )}

        {!list.isLoading && !sorted.length ? (
          <div className="text-sm text-fg-muted">{t('files:emptyFolder')}</div>
        ) : (
          <div className="space-y-1">
            {!isRoot && (
              <button
                className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm text-fg-muted transition-colors hover:bg-bg-card hover:text-fg"
                onClick={() => nav(buildSearch(parentPath))}
              >
                <FolderUp size={16} className="text-fg-subtle" />
                <span>..</span>
              </button>
            )}
            {sorted.map((e) => (
              <div
                key={e.path}
                className="group flex items-center gap-3 rounded-lg px-3 py-2 transition-colors hover:bg-bg-card"
              >
                <div className="shrink-0 text-fg-subtle">
                  {e.type === 'dir' ? <Folder size={18} className="text-warn" /> : <File size={18} className="text-brand" />}
                </div>
                <button
                  className="min-w-0 flex-1 truncate text-left text-sm font-medium text-fg hover:underline"
                  onClick={() => {
                    if (e.type === 'dir') nav(buildSearch(e.path));
                    else {
                      // download file
                      window.open(`/api/files/download?path=${encodeURIComponent(e.path)}`, '_blank');
                    }
                  }}
                >
                  {e.name}
                </button>
                <div className="hidden items-center gap-4 text-xs text-fg-muted md:flex">
                  <span className="w-24 text-right">{e.type === 'dir' ? t('files:type.dir') : fmtSize(e.size)}</span>
                  <span className="w-36 text-right">{fmtDate(e.modTime)}</span>
                </div>
                <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                  <button
                    className="btn-ghost p-1"
                    title={t('files:actions.download')}
                    onClick={() => window.open(`/api/files/download?path=${encodeURIComponent(e.path)}`, '_blank')}
                  >
                    <Download size={14} />
                  </button>
                  <button
                    className="btn-ghost p-1"
                    title={t('files:actions.rename')}
                    onClick={() => {
                      setRenameTarget(e);
                      setRenameValue(e.name);
                    }}
                  >
                    <Pencil size={14} />
                  </button>
                  <button
                    className="btn-danger p-1"
                    title={t('files:actions.delete')}
                    onClick={() => doDelete(e)}
                    disabled={deleteBusy === e.path}
                  >
                    {deleteBusy === e.path ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Uploads panel (bottom-right) */}
      {uploadJobs.length > 0 && (
        <div className={`fixed bottom-0 right-0 z-30 w-full max-w-md ${uploadsOpen ? 'h-80' : 'h-auto'} border border-border bg-bg-card shadow-soft transition-all duration-200`}>
          <div className="flex items-center justify-between border-b border-border px-4 py-2">
            <div className="flex items-center gap-2 text-sm font-medium">
              <HardDriveUpload size={14} className="text-brand" />
              {t('files:actions.upload')}
              {activeCount > 0 && (
                <span className="rounded-full bg-brand/20 px-2 py-0.5 text-xs text-brand">{activeCount}</span>
              )}
            </div>
            <div className="flex items-center gap-1">
              <button className="btn-ghost p-1 text-xs" onClick={() => setUploadsOpen((s) => !s)}>
                {uploadsOpen ? <ChevronRight size={14} className="rotate-90" /> : <ChevronLeft size={14} className="-rotate-90" />}
              </button>
              <button className="btn-ghost p-1 text-xs" onClick={() => setUploadJobs([])} title={t('common:actions.close')}>
                <X size={14} />
              </button>
            </div>
          </div>
          {uploadsOpen && (
            <div className="h-full overflow-auto p-3 pb-10">
              {uploadJobs.length === 0 && <div className="text-xs text-fg-muted">No uploads</div>}
              <div className="space-y-2">
                {uploadJobs.map((j) => {
                  const pct = j.total > 0 ? Math.round((j.loaded / j.total) * 100) : 0;
                  return (
                    <div key={j.file.name + j.jobId} className="rounded-lg border border-border bg-bg-soft p-2 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="truncate font-medium">{j.file.name}</span>
                        <span className="shrink-0 text-fg-muted">
                          {j.done ? t('files:upload.done') : j.error ? t('files:upload.error', { msg: j.error }) : `${pct}%`}
                        </span>
                      </div>
                      <div className="mt-1 h-1.5 w-full overflow-hidden rounded bg-bg">
                        <div
                          className={`h-full rounded ${j.error ? 'bg-err' : j.done ? 'bg-ok' : 'bg-brand'}`}
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                      <div className="mt-1 flex items-center justify-between text-fg-muted">
                        <span>
                          {fmtSize(j.loaded)} / {fmtSize(j.total)}
                        </span>
                        {!j.done && !j.error && (
                          <button
                            className="text-fg-muted hover:text-err"
                            onClick={() => {
                              setUploadJobs((prev) => prev.map((u) => (u.file === j.file ? { ...u, aborted: true } : u)));
                            }}
                          >
                            {t('files:upload.cancel')}
                          </button>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Rename modal */}
      {renameTarget && (
        <Modal onClose={() => setRenameTarget(null)}>
          <form onSubmit={doRename} className="space-y-3">
            <h3 className="text-sm font-semibold">{t('files:dialogs.renameTitle')}</h3>
            <p className="text-xs text-fg-muted">{renameTarget.path}</p>
            <input
              className="input"
              autoFocus
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              placeholder={t('files:dialogs.renamePlaceholder')}
            />
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-ghost" onClick={() => setRenameTarget(null)}>
                {t('common:actions.cancel')}
              </button>
              <button type="submit" className="btn-primary" disabled={renameBusy}>
                {renameBusy ? <Loader2 size={14} className="animate-spin" /> : t('common:actions.save')}
              </button>
            </div>
          </form>
        </Modal>
      )}

      {/* Mkdir modal */}
      {mkdirOpen && (
        <Modal onClose={() => setMkdirOpen(false)}>
          <div className="space-y-3">
            <h3 className="text-sm font-semibold">{t('files:dialogs.newFolderTitle')}</h3>
            <p className="text-xs text-fg-muted">{path || '/'}</p>
            <input
              className="input"
              autoFocus
              value={mkdirName}
              onChange={(e) => setMkdirName(e.target.value)}
              placeholder={t('files:dialogs.newFolderPlaceholder')}
            />
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-ghost" onClick={() => setMkdirOpen(false)}>
                {t('common:actions.cancel')}
              </button>
              <button type="button" className="btn-primary" onClick={doMkdir}>
                {t('common:actions.create')}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Remote pull modal */}
      {activeMenu === 'pull' && (
        <Modal onClose={() => setActiveMenu(null)}>
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Globe size={16} className="text-brand" />
              <h3 className="text-sm font-semibold">{t('files:dialogs.remotePullTitle')}</h3>
            </div>
            <p className="text-xs text-fg-muted">{t('files:dialogs.remotePullDesc')}</p>
            <input
              className="input"
              placeholder="https://..."
              value={pullUrl}
              onChange={(e) => setPullUrl(e.target.value)}
            />
            <input
              className="input font-mono text-xs"
              placeholder={t('files:dialogs.remotePullDest')}
              value={pullDest}
              onChange={(e) => setPullDest(e.target.value)}
            />
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-ghost" onClick={() => setActiveMenu(null)}>
                {t('common:actions.cancel')}
              </button>
              <button type="button" className="btn-primary" onClick={doPull}>
                {t('files:dialogs.remotePullSubmit')}
              </button>
            </div>
          </div>
        </Modal>
      )}

      {/* Drop notice */}
      {dropHover && (
        <div className="pointer-events-none absolute inset-0 z-40 flex flex-col items-center justify-center">
          <div className="rounded-xl border-2 border-dashed border-brand/50 bg-bg/80 px-6 py-4 text-center text-sm text-brand backdrop-blur">
            <UploadCloud size={24} className="mx-auto mb-2 text-brand" />
            {t('files:upload.dropzone')}
          </div>
        </div>
      )}

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
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

/* Modal wrapper */
function Modal({ children, onClose }: { children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div
        className="card w-full max-w-md"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          className="absolute right-3 top-3 text-fg-muted hover:text-fg"
          onClick={onClose}
          aria-label="Close"
        >
          <X size={14} />
        </button>
        {children}
      </div>
    </div>
  );
}
