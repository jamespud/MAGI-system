import { setupWorker } from 'msw/browser';
import { handlers } from './handlers';

export const worker = setupWorker(...handlers);

export async function startWorker(): Promise<void> {
  // MSW is OFF by default so the frontend talks to the real backend. Set
  // VITE_USE_MSW=true in .env (dev only) to intercept requests with mock
  // handlers. Only starts in DEV builds (never in production).
  if (import.meta.env.DEV && import.meta.env.VITE_USE_MSW === 'true') {
    await worker.start({
      onUnhandledRequest: 'bypass',
    });
    console.log('[MSW] Mock Service Worker started (VITE_USE_MSW=true)');
  }
}
