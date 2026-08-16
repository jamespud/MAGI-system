import { useEffect } from 'react';
import { Outlet, useNavigate } from 'react-router-dom';
import TopNav from './TopNav';
import LeftNav from './LeftNav';
import RightInspector from './RightInspector';
import BottomTimeline from './BottomTimeline';
import { useCaseStore } from '@/stores';
import { UNAUTHORIZED_EVENT } from '@/api/client';

export default function AppShell() {
  const cases = useCaseStore((s) => s.cases);
  const navigate = useNavigate();

  // P0: D1 — when any API call returns 401 (invalid/expired API key), route the
  // user to the login page. The watcher only lives inside the authenticated
  // shell, so it never fires a redirect loop from the /login page itself.
  useEffect(() => {
    const onUnauthorized = () => navigate('/login', { replace: true });
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [navigate]);

  return (
    <div className="flex h-screen flex-col bg-base">
      <TopNav />
      <div className="flex flex-1 overflow-hidden">
        <LeftNav cases={cases} />
        <main className="flex-1 overflow-y-auto bg-base">
          <Outlet />
        </main>
        <RightInspector />
      </div>
      <BottomTimeline />
    </div>
  );
}
