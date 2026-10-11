import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { App } from './App'
import { ErrorBoundary } from './components/ErrorBoundary'

it('routes the application shell without ERP pages', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ status: 'ready', dependencies: { postgres: 'ok', redis: 'ok' } })))
  render(<MemoryRouter><App /></MemoryRouter>)
  expect(screen.getByRole('heading', { name: /One framework/ })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('link', { name: 'Service status' }))
  expect(await screen.findByText('All services are ready')).toBeInTheDocument()
  expect(screen.getAllByText('Connected')).toHaveLength(2)
})
it('shows loading, outage, and refresh recovery', async () => {
  vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('private network detail')).mockResolvedValueOnce(new Response(JSON.stringify({ status: 'not_ready', dependencies: { redis: 'unavailable' } }), { status: 503 }))
  render(<MemoryRouter initialEntries={['/status']}><App /></MemoryRouter>)
  expect(screen.getByRole('status')).toHaveTextContent('Checking services')
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the API')
  expect(screen.queryByText('private network detail')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Refresh status' }))
  expect(await screen.findByText('Some services are unavailable')).toBeInTheDocument()
})
it('renders a not-found route', () => {render(<MemoryRouter initialEntries={['/missing']}><App /></MemoryRouter>);expect(screen.getByRole('heading', { name: 'Page not found' })).toBeInTheDocument()})
it('contains render failures without disclosing details', () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  function Broken(): never { throw new Error('private stack') }
  render(<ErrorBoundary><Broken /></ErrorBoundary>)
  expect(screen.getByRole('alert')).toHaveTextContent('This view could not load')
  expect(screen.queryByText('private stack')).not.toBeInTheDocument()
})
