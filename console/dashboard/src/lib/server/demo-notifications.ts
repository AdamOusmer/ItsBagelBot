import type { NotificationWire } from './services';

// Shared between the /notifications page load and the layout's bell peek so
// DEMO=1 shows the same rows in both places.
export const demoNotifications: NotificationWire[] = [
  {
    id: 2,
    scope: 'broadcast',
    title: 'Scheduled maintenance tonight',
    body: 'The bot will restart briefly around midnight UTC. Commands may pause for a few seconds.',
    level: 'warning',
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 2 * 3600e3).toISOString(),
    read: false
  },
  {
    id: 1,
    scope: 'direct',
    title: 'Welcome aboard',
    body: "Thanks for joining ItsBagelBot — let us know if you run into anything.",
    level: 'info',
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 26 * 3600e3).toISOString(),
    read: true
  }
];
