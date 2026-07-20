import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './styles/globals.css';
import { startWorker } from './mock/browser';

// Start MSW only when VITE_USE_MSW=true (dev-only). Default: real backend.
startWorker().finally(() => {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
});
