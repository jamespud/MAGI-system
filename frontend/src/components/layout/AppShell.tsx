import { Outlet } from 'react-router-dom';
import TopNav from './TopNav';
import LeftNav from './LeftNav';
import { useCaseStore } from '@/stores';

export default function AppShell() {
  const cases = useCaseStore((s) => s.cases);

  return (
    <div className="flex h-screen flex-col bg-base">
      <TopNav />
      <div className="flex flex-1 overflow-hidden">
        <LeftNav cases={cases} />
        <main className="flex-1 overflow-y-auto bg-base">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
