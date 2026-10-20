import { createSequence } from './sequence.mjs';
import { routeTemplates } from './requests.mjs';

export function diagnosticSequence(name) {
  if (/^route-\d+$/.test(name)) {
    const index = Number(name.slice(6));
    if (!routeTemplates[index]) throw new Error('诊断路由编号越界');
    const category = routeTemplates[index][0];
    const route = routeTemplates.slice(0, index).filter((entry) => entry[0] === category).length;
    return (slot) => ({ category, route, ordinal: slot, routeVisit: slot });
  }
  if (name === 'write') return createSequence([{ category: 'content_write', percent: 50 }, { category: 'interaction_write', percent: 50 }]);
  const routes = { list: 0, detail: 1, feed: 2, comments: 3, user: 4 };
  if (!(name in routes)) throw new Error(`未知的诊断路由集合：${name}`);
  return (slot) => ({ category: 'read', route: routes[name], ordinal: slot, routeVisit: slot });
}
