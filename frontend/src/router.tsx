import { createBrowserRouter } from 'react-router-dom';
import { AppShell } from '@/components/layout';
import DecisionWorkspace from '@/pages/DecisionWorkspace';
import Dataset from '@/pages/Dataset';
import Memory from '@/pages/Memory';
import Approvals from '@/pages/Approvals';
import History from '@/pages/History';
import Knowledge from '@/pages/Knowledge';
import Replay from '@/pages/Replay';
import Evaluation from '@/pages/Evaluation';
import Templates from '@/pages/Templates';
import Benchmark from '@/pages/Benchmark';
import Tools from '@/pages/Tools';
import Settings from '@/pages/Settings';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <DecisionWorkspace /> },
      { path: 'case/:caseId', element: <DecisionWorkspace /> },
      { path: 'memory', element: <Memory /> },
      { path: 'replay', element: <Replay /> },
      { path: 'approvals', element: <Approvals /> },
      { path: 'evaluation', element: <Evaluation /> },
      { path: 'dataset', element: <Dataset /> },
      { path: 'tools', element: <Tools /> },
      { path: 'settings', element: <Settings /> },
      { path: 'templates', element: <Templates /> },
      { path: 'benchmark', element: <Benchmark /> },
      { path: 'history', element: <History /> },
      { path: 'knowledge', element: <Knowledge /> },
    ],
  },
]);
