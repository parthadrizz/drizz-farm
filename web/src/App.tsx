import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { CreateWizard } from './pages/CreateWizard';
import { Nodes } from './pages/Nodes';
import { Sessions } from './pages/Sessions';
import { Settings } from './pages/Settings';
import { LiveView } from './pages/LiveView';
import { GridView } from './pages/GridView';

const navItems = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/grid', label: 'Live Grid' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/settings', label: 'Settings' },
];

function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen surface-0 text-foreground">
        <nav className="glass-panel sticky top-0 z-50">
          <div className="max-w-7xl mx-auto px-6 flex items-center h-14 gap-8">
            <span className="text-sm font-bold tracking-tight font-mono select-none">
              <span className="text-primary">drizz</span>
              <span className="text-muted-foreground">-farm</span>
            </span>
            <div className="flex gap-0.5">
              {navItems.map(({ to, label, end }) => (
                <NavLink key={to} to={to} end={end} className={({ isActive }) =>
                  `nav-link ${isActive ? 'nav-link-active' : ''}`
                }>{label}</NavLink>
              ))}
            </div>
          </div>
        </nav>
        <main className="max-w-7xl mx-auto px-6 py-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/create" element={<CreateWizard />} />
            <Route path="/nodes" element={<Nodes />} />
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
