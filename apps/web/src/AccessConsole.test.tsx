import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { AccessConsole } from './AccessConsole'

const tenantID = '11111111-1111-1111-1111-111111111111'
const session = { user: { id: '22222222-2222-2222-2222-222222222222', name: 'Local owner' }, tenants: [{ id: tenantID, name: 'Development workspace' }] }
it('connects, verifies a server-authorized workspace, and clears the in-memory session', async () => {
  const transport = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify(session))).mockResolvedValueOnce(new Response(JSON.stringify({ tenant_id: tenantID, roles: ['owner'], decision: { allow: true, decision_id: 'decision', policy_version: 'm1-native-v1' } })))
  const storage = vi.spyOn(Storage.prototype, 'setItem')
  render(<AccessConsole />)
  await userEvent.type(screen.getByLabelText('API access token'), 'private-test-token')
  await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
  expect(await screen.findByRole('heading', { name: 'Local owner' })).toBeInTheDocument()
  await userEvent.selectOptions(screen.getByLabelText('Workspace'), tenantID)
  expect(await screen.findByText('Access verified · owner')).toBeInTheDocument()
  expect(transport.mock.calls[0]?.[1]?.headers).toEqual({ Accept: 'application/json', Authorization: 'Bearer private-test-token' })
  expect(JSON.stringify(transport.mock.calls.map((c) => c[0]))).not.toContain('private-test-token')
  expect(storage).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
  expect(screen.getByLabelText('API access token')).toHaveValue('')
  expect(screen.queryByText('Local owner')).not.toBeInTheDocument()
})
it('redacts failed authentication and clears the submitted token', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('private driver error', { status: 401 }))
  render(<AccessConsole />)
  await userEvent.type(screen.getByLabelText('API access token'), 'private-test-token')
  await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not authenticate')
  expect(screen.getByLabelText('API access token')).toHaveValue('')
  expect(screen.queryByText('private driver error')).not.toBeInTheDocument()
})
it('clears an expired or disabled session during tenant verification', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify(session))).mockResolvedValueOnce(new Response('', { status: 401 }))
  render(<AccessConsole />)
  await userEvent.type(screen.getByLabelText('API access token'), 'test-token')
  await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
  await userEvent.selectOptions(await screen.findByLabelText('Workspace'), tenantID)
  expect(await screen.findByRole('alert')).toHaveTextContent('expired or was revoked')
  expect(screen.queryByText('Local owner')).not.toBeInTheDocument()
})
