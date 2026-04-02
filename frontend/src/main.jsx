import { StrictMode, Component } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.jsx'
import { AuthProvider } from './contexts/AuthContext'
import { OrgProvider } from './contexts/OrgContext'

class ErrorBoundary extends Component {
  constructor(props) { super(props); this.state = { hasError: false, error: null }; }
  static getDerivedStateFromError(error) { return { hasError: true, error }; }
  componentDidCatch(e, info) { console.error('REACT_CRASH:', e, info); }
  render() {
    if (this.state.hasError) return (
      <div style={{padding: '40px', color: '#ff6b6b', background: '#1a1a2e', minHeight: '100vh', fontFamily: 'monospace'}}>
        <h1>App Crashed</h1>
        <pre style={{whiteSpace: 'pre-wrap', color: '#ffd93d'}}>{this.state.error?.toString()}</pre>
        <button onClick={() => { localStorage.clear(); window.location.reload(); }}
          style={{marginTop: '20px', padding: '10px 20px', background: '#e94560', border: 'none', color: '#fff', borderRadius: '8px', cursor: 'pointer'}}>
          Clear Session and Reload
        </button>
      </div>
    );
    return this.props.children;
  }
}

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <ErrorBoundary>
      <AuthProvider>
        <OrgProvider>
          <App />
        </OrgProvider>
      </AuthProvider>
    </ErrorBoundary>
  </StrictMode>,
)
