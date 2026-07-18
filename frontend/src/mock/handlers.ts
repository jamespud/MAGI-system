import { http, HttpResponse } from 'msw';
import { createMockCase, createMockCaseList } from './data';

const CASE = createMockCase();
const CASE_LIST = createMockCaseList();

export const handlers = [
  http.get('/api/v1/cases', () => {
    return HttpResponse.json({ cases: CASE_LIST });
  }),

  http.get('/api/v1/cases/:id', ({ params }) => {
    if (params.id === CASE.id) {
      return HttpResponse.json(CASE);
    }
    return HttpResponse.json({ error: 'Case not found' }, { status: 404 });
  }),

  http.post('/api/v1/cases', async ({ request }) => {
    const body = await request.json();
    return HttpResponse.json({ ...CASE, ...(body as object), id: 'case-new', status: 'DRAFT' }, { status: 201 });
  }),

  http.get('/api/v1/cases/:id/stream', () => {
    return HttpResponse.json({ message: 'SSE endpoint — mock not yet connected' }, { status: 200 });
  }),
];
