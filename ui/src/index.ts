import { defineModule } from '@prusa/connect-ui/slots';

import { holdHost } from './host';
import { ZeitrafferSection } from './zeitraffer-section';
import { ZeitrafferTab } from './zeitraffer-tab';
import './styles.css';

export default defineModule((host) => {
  holdHost(host);

  host.register.deviceTab('zeitraffer', ZeitrafferTab);
  host.register.deviceSection('zeitraffer', ZeitrafferSection);
});
