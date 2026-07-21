import { describe, it, expect } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CheckCircle } from 'lucide-react';
import PaginatedSection from '../PaginatedSection';
import type { CaseSummary } from '@/types/case';

const mkCases = (n: number): CaseSummary[] =>
  Array.from({ length: n }, (_, i) => ({
    id: `c-${i}`, question: `Question ${i}`, status: 'RESOLVED', round: 1, createdAt: 't', pinned: false,
  }));

const renderSection = (items: CaseSummary[]) =>
  render(<MemoryRouter><PaginatedSection title="Completed" icon={CheckCircle} items={items} /></MemoryRouter>);

describe('PaginatedSection', () => {
  it('shows first page items and hides pager when items fit one page', () => {
    const { getByText, queryByText } = renderSection(mkCases(5));
    expect(getByText('Question 0')).toBeDefined();
    expect(getByText('Question 4')).toBeDefined();
    expect(queryByText(/\/2/)).toBeNull();
  });

  it('paginates with arrow buttons', () => {
    const { getByText, queryByText, getByLabelText } = renderSection(mkCases(12));
    expect(getByText('1/2')).toBeDefined();
    expect(getByText('Question 0')).toBeDefined();
    expect(queryByText('Question 10')).toBeNull();

    fireEvent.click(getByLabelText('Next Completed page'));
    expect(getByText('Question 10')).toBeDefined();
    expect(getByText('2/2')).toBeDefined();

    fireEvent.click(getByLabelText('Previous Completed page'));
    expect(getByText('Question 0')).toBeDefined();
  });

  it('disables prev on first page and next on last page', () => {
    const { getByLabelText } = renderSection(mkCases(12));
    expect(getByLabelText('Previous Completed page')).toHaveProperty('disabled', true);
    fireEvent.click(getByLabelText('Next Completed page'));
    expect(getByLabelText('Next Completed page')).toHaveProperty('disabled', true);
  });

  it('wheel down advances to next page', () => {
    const { getByText, container } = renderSection(mkCases(12));
    const listEl = container.querySelector('[data-testid="page-list"]') as HTMLElement;
    fireEvent.wheel(listEl, { deltaY: 100 });
    expect(getByText('Question 10')).toBeDefined();
  });

  it('wheel up goes back to previous page', () => {
    const { getByText, container } = renderSection(mkCases(12));
    const listEl = container.querySelector('[data-testid="page-list"]') as HTMLElement;
    fireEvent.wheel(listEl, { deltaY: 100 }); // page 2
    fireEvent.wheel(listEl, { deltaY: -100 }); // back to page 1
    expect(getByText('Question 0')).toBeDefined();
  });
});
