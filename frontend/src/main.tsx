import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { startWorker } from './mock/browser';
import './styles/globals.css';

async function init() {
  await startWorker();

  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}

init();
