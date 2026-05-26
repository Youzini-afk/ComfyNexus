import { useTranslation } from 'react-i18next';
import { Globe } from 'lucide-react';
import { post } from '@/lib/api';
import { setLocale, type Locale } from '@/lib/i18n';

export function LocaleSwitcher() {
  const { t, i18n } = useTranslation('common');
  const current = (i18n.resolvedLanguage ?? 'zh-CN') as Locale;
  async function pick(l: Locale) {
    setLocale(l);
    try {
      await post('/api/auth/locale', { locale: l });
    } catch {
      /* ignored: anonymous users can't persist */
    }
  }
  return (
    <div className="flex items-center gap-1 text-xs">
      <Globe size={12} className="text-fg-muted" />
      <button
        className={
          current === 'zh-CN'
            ? 'rounded px-1.5 py-0.5 bg-bg-card text-fg'
            : 'rounded px-1.5 py-0.5 text-fg-muted hover:text-fg'
        }
        onClick={() => pick('zh-CN')}
      >
        {t('locale.zh-CN')}
      </button>
      <span className="text-fg-subtle">·</span>
      <button
        className={
          current === 'en-US'
            ? 'rounded px-1.5 py-0.5 bg-bg-card text-fg'
            : 'rounded px-1.5 py-0.5 text-fg-muted hover:text-fg'
        }
        onClick={() => pick('en-US')}
      >
        {t('locale.en-US')}
      </button>
    </div>
  );
}
