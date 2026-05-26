import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { post, ApiError } from '@/lib/api';
import { LocaleSwitcher } from '@/components/LocaleSwitcher';

export function LoginPage() {
  const { t } = useTranslation(['auth', 'common', 'errors']);
  const nav = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [totp, setTotp] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await post('/api/auth/login', { username, password, totp });
      nav('/workbench');
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
          <div className="flex items-center gap-2">
            <Logo />
            <span className="text-lg font-semibold text-fg">
              {t('common:appName')}
            </span>
          </div>
          <LocaleSwitcher />
        </div>
        <div className="card">
          <h1 className="mb-1 text-xl font-semibold text-fg">
            {t('auth:login.title')}
          </h1>
          <p className="mb-6 text-sm text-fg-muted">
            {t('auth:login.subtitle')}
          </p>

          <form onSubmit={submit} className="space-y-4">
            <div>
              <label className="label">{t('auth:login.username')}</label>
              <input
                className="input"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoFocus
                autoComplete="username"
                required
              />
            </div>
            <div>
              <label className="label">{t('auth:login.password')}</label>
              <input
                className="input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </div>
            <div>
              <label className="label">{t('auth:login.totp')}</label>
              <input
                className="input tracking-widest"
                inputMode="numeric"
                pattern="\d{6}"
                maxLength={6}
                value={totp}
                onChange={(e) =>
                  setTotp(e.target.value.replace(/[^0-9]/g, '').slice(0, 6))
                }
                placeholder="••••••"
                autoComplete="one-time-code"
                required
              />
              <p className="mt-1 text-xs text-fg-subtle">
                {t('auth:login.totpHint')}
              </p>
            </div>
            {err && (
              <div className="rounded-lg border border-err/30 bg-err/10 px-3 py-2 text-sm text-err">
                {err}
              </div>
            )}
            <button
              className="btn-primary w-full"
              type="submit"
              disabled={busy}
            >
              {busy ? t('common:actions.loading') : t('auth:login.submit')}
            </button>
          </form>
        </div>
        <p className="mt-3 text-center text-xs text-fg-subtle">
          <Link to="/setup" className="hover:text-fg">
            {t('auth:setup.title')}
          </Link>
        </p>
      </div>
    </div>
  );
}

function Logo() {
  return (
    <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-brand to-ok">
      <span className="text-base font-bold text-white">C</span>
    </div>
  );
}
