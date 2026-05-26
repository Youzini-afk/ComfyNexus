import { useTranslation } from 'react-i18next';
import { HardHat, Box, Image, Cpu, Settings, Layers } from 'lucide-react';

export function ModelsPage() {
  const { t } = useTranslation('models');
  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center gap-2">
        <Box size={20} className="text-brand" />
        <h1 className="text-xl font-semibold">{t('title')}</h1>
      </div>
      <p className="text-sm text-fg-muted">{t('subtitle')}</p>
      <PlaceholderCard icon={<Layers size={28} />} label={t('comingSoon')} />
    </div>
  );
}

export function ImagesPage() {
  const { t } = useTranslation('images');
  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center gap-2">
        <Image size={20} className="text-brand" />
        <h1 className="text-xl font-semibold">{t('title')}</h1>
      </div>
      <p className="text-sm text-fg-muted">{t('subtitle')}</p>
      <PlaceholderCard icon={<Image size={28} />} label={t('comingSoon')} />
    </div>
  );
}

export function SystemPage() {
  const { t } = useTranslation('system');
  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center gap-2">
        <Cpu size={20} className="text-brand" />
        <h1 className="text-xl font-semibold">{t('title')}</h1>
      </div>
      <p className="text-sm text-fg-muted">{t('subtitle')}</p>
      <PlaceholderCard icon={<HardHat size={28} />} label={t('comingSoon')} />
    </div>
  );
}

export function SettingsPage() {
  const { t } = useTranslation('settings');
  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center gap-2">
        <Settings size={20} className="text-brand" />
        <h1 className="text-xl font-semibold">{t('title')}</h1>
      </div>
      <div className="card flex items-center justify-between">
        <span className="text-sm">{t('language')}</span>
        <LocaleSwitcher />
      </div>
      <PlaceholderCard icon={<Settings size={28} />} label="" />
    </div>
  );
}

import { LocaleSwitcher } from '@/components/LocaleSwitcher';

function PlaceholderCard({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="card flex flex-col items-center justify-center gap-3 py-12 text-fg-muted">
      {icon}
      <span className="text-sm">{label}</span>
    </div>
  );
}
