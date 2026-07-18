import { createBrowserRouter } from 'react-router-dom';
import { AppShell } from '@/components/layout';
import DecisionWorkspace from '@/pages/DecisionWorkspace';
import PlaceholderPage from '@/pages/PlaceholderPage';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <DecisionWorkspace /> },
      { path: 'case/:caseId', element: <DecisionWorkspace /> },
      { path: 'memory', element: <PlaceholderPage title="Memory" /> },
      { path: 'replay', element: <PlaceholderPage title="Replay" /> },
      { path: 'evaluation', element: <PlaceholderPage title="Evaluation" /> },
      { path: 'dataset', element: <PlaceholderPage title="Dataset" /> },
      { path: 'tools', element: <PlaceholderPage title="Tools" /> },
      { path: 'settings', element: <PlaceholderPage title="Settings" /> },
      { path: 'templates', element: <PlaceholderPage title="Templates" /> },
      { path: 'benchmark', element: <PlaceholderPage title="Benchmark" /> },
      { path: 'history', element: <PlaceholderPage title="History" /> },
    ],
  },
]);
