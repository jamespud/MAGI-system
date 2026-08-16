import { describe, it, expect, beforeEach } from 'vitest';
import { useAuthStore } from '../authStore';
import { clearApiKey, getApiKey, setApiKey as persistKey } from '@/api/client';

describe('authStore', () => {
  beforeEach(() => {
    clearApiKey();
    useAuthStore.setState({ apiKey: '', hasKey: false });
  });

  it('starts without a key', () => {
    expect(useAuthStore.getState().hasKey).toBe(false);
    expect(useAuthStore.getState().apiKey).toBe('');
  });

  it('setApiKey persists to localStorage and updates state', () => {
    useAuthStore.getState().setApiKey('sk-abc');
    expect(useAuthStore.getState().hasKey).toBe(true);
    expect(useAuthStore.getState().apiKey).toBe('sk-abc');
    expect(getApiKey()).toBe('sk-abc');
    expect(localStorage.getItem('magi.apiKey')).toBe('sk-abc');
  });

  it('clearApiKey removes the persisted key', () => {
    useAuthStore.getState().setApiKey('sk-abc');
    useAuthStore.getState().clearApiKey();
    expect(useAuthStore.getState().hasKey).toBe(false);
    expect(getApiKey()).toBe('');
  });

  it('hydrates from an existing persisted key', () => {
    persistKey('sk-existing');
    useAuthStore.setState({ apiKey: getApiKey(), hasKey: getApiKey().length > 0 });
    expect(useAuthStore.getState().hasKey).toBe(true);
    expect(useAuthStore.getState().apiKey).toBe('sk-existing');
  });
});
