import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ErrorBoundary } from './components/ErrorBoundary'
import { captureLoginReturn } from './auth/client'
import { App } from './App'
import './style.css'

const loginReturn = captureLoginReturn(window.location, window.history)

createRoot(document.getElementById('root')!).render(<StrictMode><ErrorBoundary><BrowserRouter><App loginReturn={loginReturn} /></BrowserRouter></ErrorBoundary></StrictMode>)
