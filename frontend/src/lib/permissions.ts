// Permission predicates for the SPA. They mirror api.Caller's helpers in
// backend/api/caller.go.
//
// currentUser is null on the desktop build — it has no login and every action
// runs as the implicit system user, so a null user is treated as fully
// privileged. On mosaicd, an admin implicitly holds every permission.
import type {UserDTO} from './bindings';

export const isAdmin = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin';

export const canManageUsers = (u: UserDTO | null): boolean =>
  u !== null && u.role === 'admin';

export const canAddTorrents = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin' || u.perm_add_torrents;

export const canManageRSS = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin' || u.perm_manage_rss;

export const canManageCatTags = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin' || u.perm_manage_cat_tags;

export const canChangeSettings = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin' || u.perm_change_settings;

export const canShare = (u: UserDTO | null): boolean =>
  u === null || u.role === 'admin' || u.perm_share;
