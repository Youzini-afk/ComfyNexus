import { useTranslation } from 'react-i18next';
import { Settings } from 'lucide-react';
import { LocaleSwitcher } from '@/components/LocaleSwitcher';

export { SystemPage } from './SystemPage';

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

function PlaceholderCard({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="card flex flex-col items-center justify-center gap-3 py-12 text-fg-muted">
      {icon}
      <span className="text-sm">{label}</span>
    </div>
  );
}
