import { ownedIndexFor, postIndexFor } from './sequence.mjs';

export const routeTemplates = [
  ['read', 'GET', 'GET /api/v1/posts'],
  ['read', 'GET', 'GET /api/v1/posts/:postId'],
  ['read', 'GET', 'GET /api/v1/posts/following'],
  ['read', 'GET', 'GET /api/v1/posts/:postId/comments'],
  ['read', 'GET', 'GET /api/v1/users/:username'],
  ['search', 'GET', 'GET /api/v1/search/posts'],
  ['search', 'GET', 'GET /api/v1/search/users'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/notifications'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/bookmarks'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/users/me/following'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/users/me/followers'],
  ['content_write', 'POST', 'POST /api/v1/posts'],
  ['content_write', 'POST', 'POST /api/v1/posts/:postId/comments'],
  ['content_write', 'PATCH', 'PATCH /api/v1/posts/:postId'],
  ['content_write', 'DELETE', 'DELETE /api/v1/posts/:postId'],
  ['interaction_write', 'PUT', 'PUT /api/v1/posts/:postId/like'],
  ['interaction_write', 'DELETE', 'DELETE /api/v1/posts/:postId/like'],
  ['interaction_write', 'PUT', 'PUT /api/v1/users/:userId/follow'],
  ['interaction_write', 'DELETE', 'DELETE /api/v1/users/:userId/follow'],
  ['interaction_write', 'PUT', 'PUT /api/v1/posts/:postId/bookmark'],
  ['identity_session', 'POST', 'POST /api/v1/auth/login'],
  ['identity_session', 'GET', 'GET /api/v1/users/me'],
];

export function createOperation({ readPostIDs, interactionPostIDs, editablePostIDs, deletePostIDs, corpusUsers, credentialUsers, credentialPassword, profile, hotPosts = 16, strictDelete = true, vu = () => 0 }) {
  return function operation(slot, userIndex, selection, users) {
    const category = selection.category;
    const postID = readPostIDs[postIndexFor(profile === 'smooth-mixed' ? selection.ordinal : slot, readPostIDs.length, profile, hotPosts)];
    const user = corpusUsers[indexFor(slot, corpusUsers.length)];
    if (category === 'read') {
      switch (selection.route) {
        case 0: return get(category, 'GET /api/v1/posts', '/api/v1/posts?limit=20', [200]);
        case 1: return get(category, 'GET /api/v1/posts/:postId', `/api/v1/posts/${postID}`, [200]);
        case 2: return get(category, 'GET /api/v1/posts/following', '/api/v1/posts/following?limit=20', [200]);
        case 3: return get(category, 'GET /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments?limit=20`, [200]);
        default: return get(category, 'GET /api/v1/users/:username', `/api/v1/users/${encodeURIComponent(user.username)}`, [200]);
      }
    }
    if (category === 'search') {
      const queries = ['phase18', `post ${String((slot % 50000) + 1).padStart(5, '0')}`, 'go'];
      const query = encodeURIComponent(queries[selection.ordinal % queries.length]);
      if (selection.route === 0) return get(category, 'GET /api/v1/search/posts', `/api/v1/search/posts?q=${query}&limit=20`, [200]);
      return get(category, 'GET /api/v1/search/users', `/api/v1/search/users?q=${query}&limit=20`, [200]);
    }
    if (category === 'notification_bookmark_read') {
      switch (selection.route) {
        case 0: return get(category, 'GET /api/v1/notifications', '/api/v1/notifications?limit=20', [200]);
        case 1: return get(category, 'GET /api/v1/bookmarks', '/api/v1/bookmarks?limit=20', [200]);
        case 2: return get(category, 'GET /api/v1/users/me/following', '/api/v1/users/me/following?limit=20', [200]);
        default: return get(category, 'GET /api/v1/users/me/followers', '/api/v1/users/me/followers?limit=20', [200]);
      }
    }
    if (category === 'content_write') {
      switch (selection.route) {
        case 0: return post(category, 'POST /api/v1/posts', '/api/v1/posts', { title: `k6 post ${vu()}-${slot}`, content: `deterministic content ${slot}` }, [201]);
        case 1: return post(category, 'POST /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments`, { content: `k6 comment ${slot}` }, [201]);
        case 2: {
          const editable = editablePostIDs[userIndex];
          const editableID = editable[ownedIndexFor(selection, users, editable.length)];
          return post(category, 'PATCH /api/v1/posts/:postId', `/api/v1/posts/${editableID}`, { title: `k6 edit ${slot}`, content: `deterministic edit ${slot}` }, [200], 'PATCH');
        }
        default: {
          const deletable = deletePostIDs[userIndex];
          const deletableID = deletable[ownedIndexFor(selection, users, deletable.length, strictDelete)];
          return post(category, 'DELETE /api/v1/posts/:postId', `/api/v1/posts/${deletableID}`, undefined, [204, 404], 'DELETE');
        }
      }
    }
    if (category === 'interaction_write') {
      const interactionPost = interactionPostIDs[indexFor(slot, interactionPostIDs.length)];
      const target = corpusUsers[(userIndex + Math.floor(slot % Math.max(1, corpusUsers.length - 1)) + 1) % corpusUsers.length].id;
      switch (selection.route) {
        case 0: return get(category, 'PUT /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'PUT');
        case 1: return get(category, 'DELETE /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'DELETE');
        case 2: return get(category, 'PUT /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [200], 'PUT');
        case 3: return get(category, 'DELETE /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [200], 'DELETE');
        default: return get(category, 'PUT /api/v1/posts/:postId/bookmark', `/api/v1/posts/${interactionPost}/bookmark`, [204], 'PUT');
      }
    }
    if (selection.route === 0) {
      const loginUser = credentialUsers[userIndex];
      return post('identity_session', 'POST /api/v1/auth/login', '/api/v1/auth/login', { username: loginUser.username, password: credentialPassword }, [200], 'POST', true);
    }
    return get('identity_session', 'GET /api/v1/users/me', '/api/v1/users/me', [200]);
  };
}

function indexFor(slot, size) {
  if (size <= 1) return 0;
  return Math.abs(slot ^ Math.floor(slot / 131071)) % size;
}

function get(category, route, path, statuses, method = 'GET') {
  return { category, route, path, statuses, method };
}

function post(category, route, path, body, statuses, method = 'POST', login = false) {
  return { category, route, path, statuses, method, login, body: body === undefined ? undefined : JSON.stringify(body) };
}
