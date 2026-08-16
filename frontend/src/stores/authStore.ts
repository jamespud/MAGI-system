import { create } from 'zustand';
import { clearApiKey, getApiKey, setApiKey } from '@/api/client';

interface AuthState {
  apiKey: string;
  hasKey: boolean;
  setApiKey: (key: string) => void;
  clearApiKey: () => void;
}

// authStore mirrors the API key stored by the client (localStorage is the
// source of truth). It exists so UI components can react to sign-in state and
// trigger sign-out without reaching into localStorage themselves (P0: D1).
export const useAuthStore = create<AuthState>((set) => ({
  apiKey: getApiKey(),
  hasKey: getApiKey().length > 0,
  setApiKey: (key) => {
    setApiKey(key);
    set({ apiKey: key, hasKey: key.length > 0 });
  },
  clearApiKey: () => {
    clearApiKey();
    set({ apiKey: '', hasKey: false });
  },
}));
