import { create } from 'zustand';
import { get, post } from './api';
import type { Locale } from './i18n';

export type Me = {
  username: string;
  role: 'admin' | 'user';
  locale: Locale;
};

type AuthState = {
  me: Me | null;
  loading: boolean;
  error: string | null;
  setupRequired: boolean | null;
  refresh: () => Promise<void>;
  checkSetup: () => Promise<void>;
  logout: () => Promise<void>;
};

export const useAuth = create<AuthState>((set) => ({
  me: null,
  loading: true,
  error: null,
  setupRequired: null,
  async refresh() {
    set({ loading: true, error: null });
    try {
      const me = await get<Me>('/api/auth/me');
      set({ me, loading: false });
    } catch {
      set({ me: null, loading: false });
    }
  },
  async checkSetup() {
    try {
      const r = await get<{ setupRequired: boolean }>(
        '/api/auth/setup-required'
      );
      set({ setupRequired: r.setupRequired });
    } catch {
      set({ setupRequired: false });
    }
  },
  async logout() {
    await post('/api/auth/logout', {});
    set({ me: null });
  },
}));
