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
      <div className="min-h-screen bg-gray-950 text-gray-100">
        <nav className="border-b border-gray-800 bg-gray-950/80 backdrop-blur sticky top-0 z-50">
          <div className="max-w-7xl mx-auto px-6 flex items-center h-14 gap-8">
            <span className="text-lg font-bold tracking-tight">
              <span className="text-emerald-400">drizz</span>
              <span className="text-gray-500">-farm</span>
            </span>
            <div className="flex gap-1">
              {[
                { to: '/', label: 'Dashboard', end: true },
                { to: '/create', label: 'Create' },
                { to: '/grid', label: 'Live Grid' },
                { to: '/sessions', label: 'Sessions' },
                { to: '/settings', label: 'Settings' },
              ].map(({ to, label, end }) => (
                <NavLink key={to} to={to} end={end} className={({ isActive }) =>
                  `px-3 py-1.5 rounded-md text-sm font-medium transition ${isActive ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-gray-200'}`
                }>{label}</NavLink>
              ))}
            </div>
          </div>
        </nav>
        <main className="max-w-7xl mx-auto px-6 py-8">
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
