import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import Login from '../Login';
import { api } from '@/api/client';
import { useAuthStore } from '@/stores';
import { clearApiKey, getApiKey } from '@/api/client';

vi.mock('@/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/client')>();
  return {
    ...actual,
    api: {
      ...actual.api,
      verifyAuth: vi.fn(),
    },
  };
});

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<div>workspace-home</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Login', () => {
  beforeEach(() => {
    clearApiKey();
    useAuthStore.setState({ apiKey: '', hasKey: false });
    vi.mocked(api.verifyAuth).mockReset();
  });

  it('renders the sign-in form', () => {
    renderLogin();
    expect(screen.getByRole('heading', { name: /MAGI · Sign in/i })).toBeTruthy();
    expect(screen.getByLabelText('API key')).toBeTruthy();
  });

  it('stores a valid key and navigates to the workspace', async () => {
    vi.mocked(api.verifyAuth).mockResolvedValue('valid');
    renderLogin();
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-good' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByText('workspace-home')).toBeTruthy());
    expect(getApiKey()).toBe('sk-good');
    expect(useAuthStore.getState().hasKey).toBe(true);
  });

  it('rejects an invalid key and keeps the session keyless', async () => {
    vi.mocked(api.verifyAuth).mockResolvedValue('invalid');
    renderLogin();
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-bad' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByText(/rejected/i)).toBeTruthy());
    expect(getApiKey()).toBe('');
    expect(useAuthStore.getState().hasKey).toBe(false);
  });

  it('open mode clears any key and navigates to the workspace', async () => {
    useAuthStore.getState().setApiKey('sk-old');
    renderLogin();
    fireEvent.click(screen.getByRole('button', { name: /Continue without a key/i }));
    await waitFor(() => expect(screen.getByText('workspace-home')).toBeTruthy());
    expect(getApiKey()).toBe('');
  });

  it('verifies the candidate key without persisting it first', async () => {
    vi.mocked(api.verifyAuth).mockResolvedValue('invalid');
    renderLogin();
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-probe' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(api.verifyAuth).toHaveBeenCalledWith('sk-probe'));
    expect(getApiKey()).toBe('');
  });

  it('does not persist a candidate key when verification fails with a network error', async () => {
    vi.mocked(api.verifyAuth).mockRejectedValue(new Error('network down'));
    renderLogin();
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-candidate' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByText(/could not reach the server/i)).toBeTruthy());
    expect(getApiKey()).toBe('');
    expect(useAuthStore.getState().hasKey).toBe(false);
  });

  it('treats a server-side verification outage as unavailable, not a bad key', async () => {
    vi.mocked(api.verifyAuth).mockResolvedValue('unavailable');
    renderLogin();
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-maybe-good' } });
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(screen.getByText(/could not reach the server/i)).toBeTruthy());
    // The candidate stays in the input and is NOT persisted or cleared.
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('sk-maybe-good');
    expect(getApiKey()).toBe('');
    expect(useAuthStore.getState().hasKey).toBe(false);
  });
});
