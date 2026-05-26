import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Pencil, Trash2, Power, FlaskConical } from 'lucide-react';
import { del, get, post, put, ApiError } from '@/lib/api';

type Instance = {
  id: number;
  name: string;
  sshHost: string;
  sshPort: number;
  sshUser: string;
  sshKeySource: 'inline' | 'mounted';
  sshKeyPath?: string;
  hasInlineKey: boolean;
  hasPassphrase: boolean;
  hostFingerprint?: string;
  comfyHost: string;
  comfyPort: number;
  comfyRoot?: string;
  comfyStartCmd?: string;
  notes?: string;
  isActive: boolean;
};

export function InstancesPage() {
  const { t } = useTranslation(['instances', 'common']);
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ['instances'],
    queryFn: () => get<Instance[]>('/api/instances'),
  });
  const [editing, setEditing] = useState<Instance | null>(null);
  const [showForm, setShowForm] = useState(false);

  const activate = useMutation({
    mutationFn: (id: number) => post(`/api/instances/${id}/activate`, {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['instances'] });
      void qc.invalidateQueries({ queryKey: ['active-instance'] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => del(`/api/instances/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['instances'] });
      void qc.invalidateQueries({ queryKey: ['active-instance'] });
    },
  });
  const test = useMutation({
    mutationFn: async (id: number) =>
      post<{ ok: boolean; output?: string; error?: string }>(
        `/api/instances/${id}/test`,
        {}
      ),
  });

  return (
    <div className="space-y-4 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t('instances:title')}</h1>
          <p className="text-sm text-fg-muted">{t('instances:subtitle')}</p>
        </div>
        <button
          className="btn-primary"
          onClick={() => {
            setEditing(null);
            setShowForm(true);
          }}
        >
          <Plus size={14} />
          {t('instances:addInstance')}
        </button>
      </header>

      {list.isLoading && (
        <div className="text-fg-muted">{t('common:actions.loading')}</div>
      )}
      {list.data?.length === 0 && (
        <div className="card text-center text-fg-muted">{t('instances:empty')}</div>
      )}

      <div className="space-y-3">
        {list.data?.map((inst) => (
          <div key={inst.id} className="card">
            <div className="flex items-start justify-between">
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="font-medium">{inst.name}</h2>
                  {inst.isActive && (
                    <span className="badge badge-active">
                      {t('common:status.active')}
                    </span>
                  )}
                </div>
                <div className="mt-1 font-mono text-xs text-fg-muted">
                  {inst.sshUser}@{inst.sshHost}:{inst.sshPort}
                  {' → '}
                  {inst.comfyHost}:{inst.comfyPort}
                </div>
                {inst.notes && (
                  <p className="mt-2 text-xs text-fg-subtle">{inst.notes}</p>
                )}
              </div>
              <div className="flex gap-1">
                <button
                  className="btn-ghost"
                  onClick={() => test.mutate(inst.id)}
                  title={t('common:actions.test')}
                >
                  <FlaskConical size={14} />
                </button>
                {!inst.isActive && (
                  <button
                    className="btn-ghost"
                    onClick={() => activate.mutate(inst.id)}
                    title={t('common:actions.activate')}
                  >
                    <Power size={14} />
                  </button>
                )}
                <button
                  className="btn-ghost"
                  onClick={() => {
                    setEditing(inst);
                    setShowForm(true);
                  }}
                  title={t('common:actions.edit')}
                >
                  <Pencil size={14} />
                </button>
                <button
                  className="btn-danger"
                  onClick={() => {
                    if (confirm(`Delete ${inst.name}?`)) remove.mutate(inst.id);
                  }}
                  title={t('common:actions.delete')}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            </div>
            {test.isPending && test.variables === inst.id && (
              <div className="mt-3 text-xs text-fg-muted">
                {t('common:actions.loading')}
              </div>
            )}
            {test.data && test.variables === inst.id && (
              <div
                className={`mt-3 rounded-lg border px-3 py-2 text-xs ${
                  test.data.ok
                    ? 'border-ok/30 bg-ok/10 text-ok'
                    : 'border-err/30 bg-err/10 text-err'
                }`}
              >
                {test.data.ok
                  ? `${t('instances:testOk')}\n${test.data.output ?? ''}`
                  : t('instances:testFailed', { err: test.data.error ?? '' })}
              </div>
            )}
          </div>
        ))}
      </div>

      {showForm && (
        <InstanceForm
          initial={editing}
          onClose={() => setShowForm(false)}
          onSaved={() => {
            setShowForm(false);
            void qc.invalidateQueries({ queryKey: ['instances'] });
            void qc.invalidateQueries({ queryKey: ['active-instance'] });
          }}
        />
      )}
    </div>
  );
}

function InstanceForm({
  initial,
  onClose,
  onSaved,
}: {
  initial: Instance | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation(['instances', 'common', 'errors']);
  const [name, setName] = useState(initial?.name ?? '');
  const [sshHost, setSshHost] = useState(initial?.sshHost ?? '');
  const [sshPort, setSshPort] = useState(initial?.sshPort ?? 22);
  const [sshUser, setSshUser] = useState(initial?.sshUser ?? 'root');
  const [sshKeySource, setSshKeySource] = useState<'inline' | 'mounted'>(
    initial?.sshKeySource ?? 'inline'
  );
  const [sshKeyPEM, setSshKeyPEM] = useState('');
  const [sshKeyPath, setSshKeyPath] = useState(initial?.sshKeyPath ?? '');
  const [sshPassphrase, setSshPassphrase] = useState('');
  const [hostFingerprint, setHostFingerprint] = useState(
    initial?.hostFingerprint ?? ''
  );
  const [comfyHost, setComfyHost] = useState(
    initial?.comfyHost ?? '127.0.0.1'
  );
  const [comfyPort, setComfyPort] = useState(initial?.comfyPort ?? 8188);
  const [comfyRoot, setComfyRoot] = useState(initial?.comfyRoot ?? '');
  const [comfyStartCmd, setComfyStartCmd] = useState(
    initial?.comfyStartCmd ?? ''
  );
  const [notes, setNotes] = useState(initial?.notes ?? '');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function onFile(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (!f) return;
    const text = await f.text();
    setSshKeyPEM(text);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    const body = {
      name,
      sshHost,
      sshPort,
      sshUser,
      sshKeySource,
      sshKeyPEM: sshKeySource === 'inline' ? sshKeyPEM : '',
      sshKeyPath: sshKeySource === 'mounted' ? sshKeyPath : '',
      sshPassphrase,
      hostFingerprint,
      comfyHost,
      comfyPort,
      comfyRoot,
      comfyStartCmd,
      notes,
    };
    try {
      if (initial) await put(`/api/instances/${initial.id}`, body);
      else await post('/api/instances', body);
      onSaved();
    } catch (e) {
      if (e instanceof ApiError) setErr(t(`errors:${e.code}`, e.message));
      else setErr(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <form
        onSubmit={submit}
        className="card max-h-[90vh] w-full max-w-2xl space-y-3 overflow-auto"
      >
        <h2 className="text-lg font-semibold">
          {initial ? t('instances:editInstance') : t('instances:addInstance')}
        </h2>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">{t('instances:fields.name')}</label>
            <input
              className="input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>
          <div>
            <label className="label">{t('instances:fields.sshUser')}</label>
            <input
              className="input"
              value={sshUser}
              onChange={(e) => setSshUser(e.target.value)}
              required
            />
          </div>
          <div>
            <label className="label">{t('instances:fields.sshHost')}</label>
            <input
              className="input"
              value={sshHost}
              onChange={(e) => setSshHost(e.target.value)}
              required
            />
          </div>
          <div>
            <label className="label">{t('instances:fields.sshPort')}</label>
            <input
              className="input"
              type="number"
              value={sshPort}
              onChange={(e) => setSshPort(Number(e.target.value))}
              required
            />
          </div>
          <div className="col-span-2">
            <label className="label">
              {t('instances:fields.sshKeySource')}
            </label>
            <div className="flex gap-2">
              <label className="flex items-center gap-1 text-xs">
                <input
                  type="radio"
                  checked={sshKeySource === 'inline'}
                  onChange={() => setSshKeySource('inline')}
                />
                {t('instances:keySource.inline')}
              </label>
              <label className="flex items-center gap-1 text-xs">
                <input
                  type="radio"
                  checked={sshKeySource === 'mounted'}
                  onChange={() => setSshKeySource('mounted')}
                />
                {t('instances:keySource.mounted')}
              </label>
            </div>
          </div>
          {sshKeySource === 'inline' && (
            <div className="col-span-2">
              <label className="label">
                {t('instances:fields.sshKeyPEM')}
              </label>
              <textarea
                className="input font-mono text-xs"
                rows={5}
                value={sshKeyPEM}
                onChange={(e) => setSshKeyPEM(e.target.value)}
                placeholder={
                  initial?.hasInlineKey
                    ? '(leave empty to keep current key)'
                    : '-----BEGIN OPENSSH PRIVATE KEY-----'
                }
              />
              <input
                type="file"
                accept=".pem,.key,id_rsa,id_ed25519,*/*"
                onChange={onFile}
                className="mt-1 text-xs"
              />
            </div>
          )}
          {sshKeySource === 'mounted' && (
            <div className="col-span-2">
              <label className="label">
                {t('instances:fields.sshKeyPath')}
              </label>
              <input
                className="input font-mono text-xs"
                value={sshKeyPath}
                onChange={(e) => setSshKeyPath(e.target.value)}
                placeholder="/secrets/keys/gpu.key"
                required
              />
            </div>
          )}
          <div className="col-span-2">
            <label className="label">
              {t('instances:fields.sshPassphrase')}
            </label>
            <input
              className="input"
              type="password"
              value={sshPassphrase}
              onChange={(e) => setSshPassphrase(e.target.value)}
              placeholder={
                initial?.hasPassphrase
                  ? '(leave empty to keep current)'
                  : ''
              }
            />
          </div>
          <div className="col-span-2">
            <label className="label">
              {t('instances:fields.hostFingerprint')}
            </label>
            <input
              className="input font-mono text-xs"
              value={hostFingerprint}
              onChange={(e) => setHostFingerprint(e.target.value)}
              placeholder="SHA256:..."
            />
          </div>
          <div>
            <label className="label">
              {t('instances:fields.comfyHost')}
            </label>
            <input
              className="input"
              value={comfyHost}
              onChange={(e) => setComfyHost(e.target.value)}
            />
          </div>
          <div>
            <label className="label">
              {t('instances:fields.comfyPort')}
            </label>
            <input
              className="input"
              type="number"
              value={comfyPort}
              onChange={(e) => setComfyPort(Number(e.target.value))}
            />
          </div>
          <div className="col-span-2">
            <label className="label">
              {t('instances:fields.comfyRoot')}
            </label>
            <input
              className="input font-mono text-xs"
              value={comfyRoot}
              onChange={(e) => setComfyRoot(e.target.value)}
              placeholder="/workspace/ComfyUI"
            />
          </div>
          <div className="col-span-2">
            <label className="label">
              {t('instances:fields.comfyStartCmd')}
            </label>
            <input
              className="input font-mono text-xs"
              value={comfyStartCmd}
              onChange={(e) => setComfyStartCmd(e.target.value)}
              placeholder="cd /workspace/ComfyUI && nohup python main.py --listen 127.0.0.1 &"
            />
          </div>
          <div className="col-span-2">
            <label className="label">{t('instances:fields.notes')}</label>
            <textarea
              className="input"
              rows={2}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </div>
        </div>
        {err && (
          <div className="rounded-lg border border-err/30 bg-err/10 px-3 py-2 text-sm text-err">
            {err}
          </div>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            {t('common:actions.cancel')}
          </button>
          <button type="submit" className="btn-primary" disabled={busy}>
            {busy ? t('common:actions.loading') : t('common:actions.save')}
          </button>
        </div>
      </form>
    </div>
  );
}
