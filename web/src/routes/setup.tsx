import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import QRCode from 'qrcode';
import { post, ApiError } from '@/lib/api';
import { LocaleSwitcher } from '@/components/LocaleSwitcher';

type SetupResp = { totpSecret: string; totpUri: string };

export function SetupPage() {
  const { t } = useTranslation(['auth', 'common', 'errors']);
  const nav = useNavigate();
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [resp, setResp] = useState<SetupResp | null>(null);
  const [qr, setQr] = useState<string | null>(null);

  useEffect(() => {
    async function guard() {
      const sr = await fetch('/api/auth/setup-required').then((r) => r.json());
      if (!sr.setupRequired) {
        nav('/login', { replace: true });
      }
    }
    void guard();
  }, [nav]);

  useEffect(() => {
    if (!resp) return;
    QRCode.toDataURL(resp.totpUri, { margin: 1, width: 220 })
      .then(setQr)
      .catch(() => setQr(null));
  }, [resp]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const r = await post<SetupResp>('/api/auth/setup', { username, password });
      setResp(r);
    } catch (e) {
      if (e instanceof ApiError) setErr(t(`errors:${e.code}`, e.message));
      else setErr(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center bg-bg p-6">
      <div className="w-full max-w-md">
        <div className="mb-6 flex items-center justify-between">
          <span className="text-lg font-semibold text-fg">
            {t('common:appName')}
          </span>
          <LocaleSwitcher />
        </div>
        <div className="card">
          <h1 className="mb-1 text-xl font-semibold text-fg">
            {t('auth:setup.title')}
          </h1>
          <p className="mb-6 text-sm text-fg-muted">
            {t('auth:setup.subtitle')}
          </p>

          {!resp && (
            <form onSubmit={submit} className="space-y-4">
              <div>
                <label className="label">{t('auth:setup.username')}</label>
                <input
                  className="input"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                />
              </div>
              <div>
                <label className="label">{t('auth:setup.password')}</label>
                <input
                  className="input"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  minLength={12}
                />
                <p className="mt-1 text-xs text-fg-subtle">
                  {t('auth:setup.passwordHint')}
                </p>
              </div>
              {err && (
                <div className="rounded-lg border border-err/30 bg-err/10 px-3 py-2 text-sm text-err">
                  {err}
                </div>
              )}
              <button className="btn-primary w-full" type="submit" disabled={busy}>
                {busy ? t('common:actions.loading') : t('auth:setup.submit')}
              </button>
            </form>
          )}

          {resp && (
            <div className="space-y-4">
              <h2 className="text-sm font-medium text-fg">
                {t('auth:setup.totp.title')}
              </h2>
              {qr && (
                <div className="flex justify-center">
                  <img
                    src={qr}
                    alt="TOTP QR"
                    className="rounded-lg border border-border bg-white p-2"
                  />
                </div>
              )}
              <div>
                <label className="label">{t('auth:setup.totp.secret')}</label>
                <code className="block break-all rounded-lg border border-border bg-bg-soft p-2 font-mono text-xs text-fg">
                  {resp.totpSecret}
                </code>
              </div>
              <p className="text-xs text-fg-muted">
                {t('auth:setup.totp.doneHint')}
              </p>
              <Link to="/login" className="btn-primary block text-center">
                {t('auth:login.submit')}
              </Link>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
