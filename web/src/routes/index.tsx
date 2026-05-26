import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

export function IndexRedirect() {
  const nav = useNavigate();

  useEffect(() => {
    async function decide() {
      const me = await fetch('/api/auth/me', {
        credentials: 'include',
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      });
      if (me.ok) {
        nav('/workbench', { replace: true });
        return;
      }
      const sr = await fetch('/api/auth/setup-required').then((r) => r.json());
      if (sr.setupRequired) {
        nav('/setup', { replace: true });
      } else {
        nav('/login', { replace: true });
      }
    }
    void decide();
  }, [nav]);

  return null;
}
