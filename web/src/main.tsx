import { createRoot } from 'react-dom/client';

import { Root } from './Shell';
import 'leaflet/dist/leaflet.css';
import './styles.css';

const root = document.getElementById('root');
if (!root) {
  throw new Error('root element missing');
}
createRoot(root).render(<Root />);
