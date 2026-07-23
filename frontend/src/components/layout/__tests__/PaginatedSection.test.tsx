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

  it('shows count badge when collapsible and collapsed', () => {
    const { getByText, queryByTestId } = render(
      <MemoryRouter>
        <PaginatedSection title="Completed" icon={CheckCircle} items={mkCases(5)} collapsible defaultExpanded={false} />
      </MemoryRouter>
    );
    expect(getByText('5')).toBeDefined(); // count badge
    expect(queryByTestId('page-list')).toBeNull(); // list hidden
  });

  it('expands and shows list on header click', () => {
    const { getByText, getByTestId, getByRole } = render(
      <MemoryRouter>
        <PaginatedSection title="Completed" icon={CheckCircle} items={mkCases(5)} collapsible defaultExpanded={false} />
      </MemoryRouter>
    );
    // Click header to expand
    fireEvent.click(getByRole('button'));
    expect(getByTestId('page-list')).toBeDefined();
    expect(getByText('Question 0')).toBeDefined();
  });

  it('collapses on second header click', () => {
    const { getByRole, queryByTestId } = render(
      <MemoryRouter>
        <PaginatedSection title="Completed" icon={CheckCircle} items={mkCases(5)} collapsible defaultExpanded />
      </MemoryRouter>
    );
    fireEvent.click(getByRole('button')); // collapse
    expect(queryByTestId('page-list')).toBeNull();
  });

  it('renders plain (non-clickable) header when collapsible but empty', () => {
    const { queryByRole, getByText } = render(
      <MemoryRouter>
        <PaginatedSection title="Completed" icon={CheckCircle} items={[]} collapsible />
      </MemoryRouter>
    );
    expect(queryByRole('button')).toBeNull();
    expect(getByText('No cases')).toBeDefined();
  });

  it('does not render count badge when not collapsible', () => {
    const { container } = render(
      <MemoryRouter>
        <PaginatedSection title="Completed" icon={CheckCircle} items={mkCases(5)} />
      </MemoryRouter>
    );
    // No button rendered, no count badge
    expect(container.querySelector('button')).toBeNull();
    expect(container.textContent).not.toContain('5');
  });
});
