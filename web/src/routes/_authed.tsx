import { type ReactNode, useEffect, useState } from 'react';
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Boxes,
  Cpu,
  Download,
  FolderTree,
  Image as ImageIcon,
  LogOut,
  Menu,
  Settings,
  Sparkles,
  Workflow,
  X,
} from 'lucide-react';
import { post } from '@/lib/api';
import { LocaleSwitcher } from '@/components/LocaleSwitcher';

type Me = { username: string; role: string; locale: string };

export function AppLayout(): ReactNode {
  const { t } = useTranslation('common');
  const nav = useNavigate();
  const location = useLocation();
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    async function guard() {
      try {
        const r = await fetch('/api/auth/me', {
          credentials: 'include',
          headers: { 'X-Requested-With': 'XMLHttpRequest' },
        });
        if (!r.ok) throw new Error('unauth');
        const data: Me = await r.json();
        setMe(data);
      } catch {
        setMe(null);
        nav('/login', { replace: true });
      } finally {
        setLoading(false);
      }
    }
    void guard();
  }, [nav]);

  async function logout() {
    await post('/api/auth/logout', {});
    nav('/login');
  }

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg text-fg">
        {t('actions.loading')}
      </div>
    );
  }

  if (!me) return null;

  const isActive = (path: string) => location.pathname === path;

  return (
    <div className="flex h-screen bg-bg text-fg">
      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 md:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 flex w-64 flex-col border-r border-border bg-bg-soft transition-transform duration-200 md:static md:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex items-center gap-2 px-4 py-4">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-brand to-ok">
            <span className="text-base font-bold text-white">C</span>
          </div>
          <div>
            <div className="text-sm font-semibold leading-none">
              {t('appName')}
            </div>
            <div className="mt-0.5 text-xs text-fg-muted">{t('tagline')}</div>
          </div>
          <button
            className="ml-auto md:hidden"
            onClick={() => setMobileOpen(false)}
            aria-label="Close sidebar"
          >
            <X size={18} className="text-fg-muted" />
          </button>
        </div>
        <nav className="mt-2 flex-1 space-y-0.5 px-2">
          <NavItem to="/workbench" icon={<Workflow size={16} />} label={t('nav.workbench')} active={isActive('/workbench')} />
          <NavItem to="/files" icon={<FolderTree size={16} />} label={t('nav.files')} active={isActive('/files')} />
          <NavItem to="/models" icon={<Boxes size={16} />} label={t('nav.models')} active={isActive('/models')} />
          <NavItem to="/images" icon={<ImageIcon size={16} />} label={t('nav.images')} active={isActive('/images')} />
          <NavItem to="/downloads" icon={<Download size={16} />} label={t('nav.downloads')} active={isActive('/downloads')} />
          <NavItem to="/system" icon={<Cpu size={16} />} label={t('nav.system')} active={isActive('/system')} />
          <div className="px-3 pt-4 text-xs uppercase tracking-wider text-fg-subtle">
            <Sparkles size={12} className="mr-1 inline" />
            Admin
          </div>
          <NavItem to="/instances" icon={<Cpu size={16} />} label={t('nav.instances')} active={isActive('/instances')} />
          <NavItem to="/settings" icon={<Settings size={16} />} label={t('nav.settings')} active={isActive('/settings')} />
        </nav>
        <div className="border-t border-border p-3">
          <div className="mb-2 flex items-center justify-between">
            <div>
              <div className="text-sm font-medium">{me.username}</div>
              <div className="text-xs text-fg-subtle">{me.role}</div>
            </div>
            <LocaleSwitcher />
          </div>
          <button onClick={logout} className="btn-ghost w-full">
            <LogOut size={14} />
            {t('actions.logout')}
          </button>
        </div>
      </aside>

      {/* Main */}
      <main className="flex-1 overflow-auto">
        {/* Mobile top bar */}
        <div className="flex items-center gap-2 border-b border-border bg-bg-soft px-4 py-3 md:hidden">
          <button onClick={() => setMobileOpen(true)} aria-label="Open sidebar">
            <Menu size={20} className="text-fg-muted" />
          </button>
          <span className="text-sm font-semibold">{t('appName')}</span>
        </div>
        <Outlet />
      </main>
    </div>
  );
}

function NavItem({
  to,
  icon,
  label,
  active,
}: {
  to: string;
  icon: ReactNode;
  label: string;
  active: boolean;
}) {
  return (
    <Link
      to={to}
      className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
        active
          ? 'bg-bg-card text-fg'
          : 'text-fg-muted hover:bg-bg-card hover:text-fg'
      }`}
    >
      {icon}
      <span>{label}</span>
    </Link>
  );
}
