import i18next from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

import zhCommon from '@/locales/zh-CN/common.json';
import zhAuth from '@/locales/zh-CN/auth.json';
import zhInstances from '@/locales/zh-CN/instances.json';
import zhWorkbench from '@/locales/zh-CN/workbench.json';
import zhFiles from '@/locales/zh-CN/files.json';
import zhModels from '@/locales/zh-CN/models.json';
import zhImages from '@/locales/zh-CN/images.json';
import zhSystem from '@/locales/zh-CN/system.json';
import zhErrors from '@/locales/zh-CN/errors.json';
import zhSettings from '@/locales/zh-CN/settings.json';
import zhDownloads from '@/locales/zh-CN/downloads.json';

import enCommon from '@/locales/en-US/common.json';
import enAuth from '@/locales/en-US/auth.json';
import enInstances from '@/locales/en-US/instances.json';
import enWorkbench from '@/locales/en-US/workbench.json';
import enFiles from '@/locales/en-US/files.json';
import enModels from '@/locales/en-US/models.json';
import enImages from '@/locales/en-US/images.json';
import enSystem from '@/locales/en-US/system.json';
import enErrors from '@/locales/en-US/errors.json';
import enSettings from '@/locales/en-US/settings.json';
import enDownloads from '@/locales/en-US/downloads.json';

export const supportedLocales = ['zh-CN', 'en-US'] as const;
export type Locale = (typeof supportedLocales)[number];

export const i18n = i18next.createInstance();

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'zh-CN',
    supportedLngs: supportedLocales as unknown as string[],
    nonExplicitSupportedLngs: true,
    defaultNS: 'common',
    ns: [
      'common',
      'auth',
      'instances',
      'workbench',
      'files',
      'models',
      'images',
      'system',
      'errors',
      'settings',
      'downloads',
    ],
    resources: {
      'zh-CN': {
        common: zhCommon,
        auth: zhAuth,
        instances: zhInstances,
        workbench: zhWorkbench,
        files: zhFiles,
        models: zhModels,
        images: zhImages,
        system: zhSystem,
        errors: zhErrors,
        settings: zhSettings,
        downloads: zhDownloads,
      },
      'en-US': {
        common: enCommon,
        auth: enAuth,
        instances: enInstances,
        workbench: enWorkbench,
        files: enFiles,
        models: enModels,
        images: enImages,
        system: enSystem,
        errors: enErrors,
        settings: enSettings,
        downloads: enDownloads,
      },
    },
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'comfynexus.locale',
    },
  });

export function setLocale(locale: Locale) {
  void i18n.changeLanguage(locale);
}
