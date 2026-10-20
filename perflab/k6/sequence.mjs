// Pure deterministic request selection, shared by k6 and offline checks.
const routeCounts = { read: 5, search: 2, notification_bookmark_read: 4, content_write: 4, interaction_write: 5, identity_session: 5 };
const profiles = ['smooth-mixed', 'burst-mixed', 'hot-repeat', 'cold-distinct'];

export function createSequence(mix, profile = 'smooth-mixed') {
  if (!profiles.includes(profile)) throw new Error(`unknown workload profile ${profile}`);
  if (profile === 'hot-repeat' || profile === 'cold-distinct') {
    return (slot) => ({ category: 'read', route: 1, routeVisit: slot, ordinal: slot });
  }
  const weights = mix.map((entry) => ({ ...entry, score: 0, count: 0 }));
  if (weights.reduce((sum, entry) => sum + entry.percent, 0) !== 100) throw new Error('mix must total 100');
  const cycle = [];
  for (let slot = 0; slot < 100; slot += 1) {
    let selected;
    if (profile === 'burst-mixed') {
      selected = weights.find((entry) => entry.count < entry.percent);
    } else {
      for (const entry of weights) entry.score += entry.percent;
      selected = weights.reduce((best, entry) => entry.score > best.score ? entry : best);
      selected.score -= 100;
    }
    cycle.push({ category: selected.category, rank: selected.count, percent: selected.percent });
    selected.count += 1;
  }
  return (slot) => {
    const entry = cycle[slot % 100];
    const ordinal = Math.floor(slot / 100) * entry.percent + entry.rank;
    const count = routeCounts[entry.category];
    const route = profile === 'burst-mixed' ? Math.floor(slot / 100) % count : ordinal % count;
    const routeVisit = profile === 'burst-mixed'
      ? Math.floor(Math.floor(slot / 100) / count) * entry.percent + entry.rank
      : Math.floor(ordinal / count);
    return { category: entry.category, ordinal, route, routeVisit };
  };
}

function gcd(a, b) { while (b) [a, b] = [b, a % b]; return a; }
function stride(size, seed) { let value = seed; while (gcd(value, size) !== 1) value += 1; return value; }

export function userIndexFor(selection, users) {
  return (selection.routeVisit * stride(users, 17) + selection.route * 7) % users;
}

export function ownedIndexFor(selection, users, posts) {
  // Each actor visits every owned target before revisiting it, independently
  // of the user's position in the actor permutation.
  return Math.floor(selection.routeVisit / users) % posts;
}

export function postIndexFor(slot, posts, profile = 'smooth-mixed', hotPosts = 16) {
  if (posts < 1) throw new Error('empty read corpus');
  if (profile === 'cold-distinct') {
    if (slot >= posts) throw new Error('cold-distinct corpus exhausted; refusing repeated targets');
    return slot;
  }
  const size = profile === 'hot-repeat' ? Math.min(posts, hotPosts) : posts;
  return (slot * stride(size, 31) + 11) % size;
}
