import { useState, useCallback } from 'react';

export type Toast = { id: number; message: string; tone?: 'ok' | 'err' };

let TOAST_ID = 0;

export function useToasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const show = useCallback((message: string, tone: 'ok' | 'err' = 'ok') => {
    const id = ++TOAST_ID;
    setToasts((prev) => [...prev, { id, message, tone }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 2800);
  }, []);
  const remove = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);
  return { toasts, show, remove };
}
