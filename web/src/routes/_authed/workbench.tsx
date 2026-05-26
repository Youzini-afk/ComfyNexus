import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { ExternalLink, AlertTriangle } from 'lucide-react';
import { get } from '@/lib/api';

export function Workbench() {
  const { t } = useTranslation(['workbench', 'common']);
  const active = useQuery({
    queryKey: ['active-instance'],
    queryFn: () => get<{ activeId: number }>('/api/instances/active'),
    refetchInterval: 30_000,
  });
  const [reload, setReload] = useState(0);

  if (active.isLoading) {
    return (
      <div className="p-6 text-fg-muted">{t('common:actions.loading')}</div>
    );
  }
  const hasActive = (active.data?.activeId ?? 0) > 0;

  if (!hasActive) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="card max-w-md text-center">
          <AlertTriangle className="mx-auto mb-3 text-warn" />
          <h2 className="mb-2 text-lg font-medium">{t('workbench:title')}</h2>
          <p className="mb-4 text-sm text-fg-muted">{t('workbench:noActive')}</p>
          <Link to="/instances" className="btn-primary">
            {t('workbench:openInstances')}
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b border-border bg-bg-soft px-4 py-2 text-sm">
        <div className="font-medium">{t('workbench:title')}</div>
        <div className="flex gap-2">
          <button
            className="btn-ghost text-xs"
            onClick={() => setReload((n) => n + 1)}
          >
            {t('common:actions.refresh')}
          </button>
          <a
            className="btn-ghost text-xs"
            href="/comfy/"
            target="_blank"
            rel="noreferrer"
          >
            <ExternalLink size={12} />
            {t('workbench:openExternal')}
          </a>
        </div>
      </div>
      <iframe
        key={reload}
        title="ComfyUI"
        src="/comfy/"
        className="flex-1 border-0 bg-bg"
      />
    </div>
  );
}
