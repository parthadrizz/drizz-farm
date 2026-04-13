import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { CreateWizard } from './pages/CreateWizard';
import { Sessions } from './pages/Sessions';
import { Settings } from './pages/Settings';
import { LiveView } from './pages/LiveView';
import { GridView } from './pages/GridView';

function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen surface-0 text-[hsl(210,14%,83%)]">
        <nav className="border-b border-[hsl(215,14%,18%)] surface-1/80 backdrop-blur-md sticky top-0 z-50">
          <div className="max-w-7xl mx-auto px-6 flex items-center h-12 gap-8">
            <span className="text-sm font-bold tracking-tight font-mono">
              <span className="text-[hsl(150,70%,42%)]">drizz</span>
              <span className="text-[hsl(215,10%,40%)]">-farm</span>
            </span>
            <div className="flex gap-0.5">
              {[
                { to: '/', label: 'Dashboard', end: true },
                { to: '/create', label: 'Create' },
                { to: '/grid', label: 'Live Grid' },
                { to: '/sessions', label: 'Sessions' },
                { to: '/settings', label: 'Settings' },
              ].map(({ to, label, end }) => (
                <NavLink key={to} to={to} end={end} className={({ isActive }) =>
                  `px-3 py-1.5 rounded-md text-[13px] font-medium transition ${
                    isActive
                      ? 'surface-2 text-white'
                      : 'text-[hsl(215,10%,50%)] hover:text-[hsl(210,14%,83%)] hover:surface-2'
                  }`
                }>{label}</NavLink>
              ))}
            </div>
          </div>
        </nav>
        <main className="max-w-7xl mx-auto px-6 py-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/create" element={<CreateWizard />} />
            <Route path="/sessions" element={<Sessions />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/grid" element={<GridView />} />
            <Route path="/live/:id" element={<LiveView />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  );
}

export default App;
